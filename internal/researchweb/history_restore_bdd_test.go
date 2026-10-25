//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

func TestHistoryRestoreBDD_IsolatedPostgresRetainsMissingArtifactsAndRechecksLean(t *testing.T) {
	container, leanFile := os.Getenv("RESEARCH_TEST_RESTORE_CONTAINER"), os.Getenv("RESEARCH_TEST_LEAN_CONFIG")
	if container == "" || leanFile == "" {
		t.Skip("dedicated PostgreSQL container and local Lean required")
	}
	if !requestPattern.MatchString(container) {
		t.Fatal("invalid dedicated container name")
	}
	dsn := postgresDSN(t)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Del("search_path")
	parsed.RawQuery = query.Encode()
	dsn = parsed.String()
	body, err := os.ReadFile(leanFile)
	if err != nil {
		t.Fatal(err)
	}
	var cfg leancheck.Config
	if err = json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	s, p := packageFixture(t)
	profile := s.profile("proof")
	if err = s.Store.Change(0, "", "", "Preparation failure", p.Target.ID, "test", func(d *Data) error {
		task, attempt := identifier("task"), identifier("run")
		d.Tasks = append(d.Tasks, Task{ID: task, Target: p.Target.ID, Kind: "proof", State: "failed", Attempt: attempt})
		d.Attempts = append(d.Attempts, Attempt{ID: attempt, TaskID: task, Target: p.Target.ID, TargetRevision: p.Target.Revision, Profile: profile.ID, ProfileConfiguration: &profile, Status: "failed", RemoteOutcome: "not_started"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p, err = s.ExportResultPackage(p.Result.ID)
	if err != nil {
		t.Fatal(err)
	}
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	o.Config.Database = DatabaseConfig{Driver: "postgres", DSNEnv: "HISTORY_TEST_DB"}
	lookup := o.Lookup
	o.Lookup = func(k string) (string, bool) {
		if k == "HISTORY_TEST_DB" {
			return dsn, true
		}
		return lookup(k)
	}
	o.Lean = &cfg
	o.Checker = leancheck.DockerChecker{Config: cfg}
	dest := serviceFor(t, o)
	act(t, dest, Action{Type: "PAUSE"})
	if err = dest.ImportResultPackage(ImportResultPackageRequest{ExpectedRevision: stateOf(t, dest).Revision, RequestID: identifier("cmd"), Package: p, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	wait := func(s *Service, id string) View {
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			v := stateOf(t, s)
			c := v.verification(id)
			if c != nil && c.Status != "queued" && c.Status != "running" {
				if c.Status != "verified" {
					t.Fatal("native check failed", c)
				}
				return v
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("native check timed out")
		return View{}
	}
	v := wait(dest, p.Verification.ID)
	if v.Results[0].Status != "ready" || v.entity(p.Target.ID).Status != "in_review" {
		t.Fatal("import lost readiness or silently accepted", v.Results)
	}
	if err = dest.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	run := func(ctx context.Context, in io.Reader, out io.Writer, args ...string) error {
		cmd := exec.CommandContext(ctx, "docker", append([]string{"exec", "-i", container}, args...)...)
		cmd.Stdin, cmd.Stdout = in, out
		return cmd.Run()
	}
	backup := filepath.Join(t.TempDir(), "backup")
	m, err := BackupPostgres(ctx, o.Config, o.Lookup, backup, run)
	if err != nil || !m.RestoreVerified {
		t.Fatal("backup verification failed", err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/research_restore_" + identifier("history")[8:]
	// Restore accepts only an explicit new database name; generated ids are hex.
	restoredDSN := u.String()
	restored := o
	restored.Config.DataDir = filepath.Join(t.TempDir(), "restored-state")
	restored.Lookup = func(k string) (string, bool) {
		if k == "HISTORY_TEST_DB" {
			return restoredDSN, true
		}
		return lookup(k)
	}
	if _, err = RestorePostgres(ctx, restored.Config, restored.Lookup, backup, run); err != nil {
		t.Fatal("restore failed", err)
	}
	r := serviceFor(t, restored)
	v = stateOf(t, r)
	missing := v.attempt(p.IncompleteAttempts[0].Attempt.ID)
	if !v.Paused || missing == nil || len(missing.MissingMaterials) != 3 || v.entity(p.Target.ID).Status != "in_review" || len(v.Attempts) != 4 {
		t.Fatal("restoration changed historical evidence", v.Revision)
	}
	if _, err = r.readInput(*missing); err == nil {
		t.Fatal("missing input fabricated")
	}
	if err = r.SubmitProofSource(p.Target.ID, ProofSourceRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Source: p.Verification.Source, Author: p.Target.ProofAuthor, Basis: "Independent local check after isolated database restore"}); err != nil {
		t.Fatal(err)
	}
	v = stateOf(t, r)
	v = wait(r, v.Verifications[len(v.Verifications)-1].ID)
	if len(v.Attempts) != 4 || v.entity(p.Target.ID).Status != "in_review" {
		t.Fatal("restore verification created model requests or acceptance")
	}
	t.Logf("restored revision %d, incomplete attempts 1, model calls after transfer 0, native rechecks 2, accepted false", m.Revision)
}
