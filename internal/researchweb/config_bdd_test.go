//go:build linux

package researchweb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/coddy"
)

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestServerBDD_ConfigurationUsesExplicitLocalBoundaries(t *testing.T) {
	dir := t.TempDir()
	base := Config{Listen: "127.0.0.1:4187", DataDir: "state", WebDir: "web", MaxParallel: 1,
		Workspaces: []Workspace{{ID: "source", Label: "Source", Path: "source"}}}
	path := filepath.Join(dir, "config.json")
	writeJSON(t, path, base)
	o, err := LoadOptions(path)
	if err != nil || o.Config.DataDir != filepath.Join(dir, "state") || o.Config.Workspaces[0].Path != filepath.Join(dir, "source") {
		t.Fatalf("relative paths: %+v %v", o.Config, err)
	}
	for _, host := range []string{"0.0.0.0:4187", "192.0.2.1:4187", "localhost:4187", "127.0.0.1:99999"} {
		t.Run(host, func(t *testing.T) {
			bad := base
			bad.Listen = host
			writeJSON(t, path, bad)
			if _, err := LoadOptions(path); err == nil {
				t.Fatal("non-numeric or non-loopback address accepted")
			}
		})
	}
	for _, state := range []string{"web", "web/private"} {
		bad := base
		bad.DataDir = state
		writeJSON(t, path, bad)
		if _, err := LoadOptions(path); err == nil {
			t.Fatal("state under web root accepted")
		}
	}
	writeJSON(t, path, map[string]any{"listen": base.Listen, "arbitrary_command": "denied"})
	if _, err := LoadOptions(path); err == nil {
		t.Fatal("unknown configuration accepted")
	}
}

func TestServerBDD_AccessKeyPersistsPrivatelyAndRejectsInvalidInput(t *testing.T) {
	c := Config{DataDir: t.TempDir()}
	first, err := AccessKey(c, nil)
	if err != nil || len(first) != 64 {
		t.Fatal("key not generated", err)
	}
	next, err := AccessKey(c, nil)
	if err != nil || next != first {
		t.Fatal("key not preserved")
	}
	path := filepath.Join(c.DataDir, "access-token")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := AccessKey(c, nil); err == nil {
		t.Fatal("public key file accepted")
	}
	c.TokenEnv = "ACCESS_KEY"
	for _, value := range []string{"", "short", strings.Repeat("a", 257), strings.Repeat("a", 40) + "\n", strings.Repeat("a", 40) + " "} {
		if _, err := AccessKey(c, func(string) (string, bool) { return value, true }); err == nil {
			t.Fatal("invalid environment key accepted")
		}
	}
	if value, err := AccessKey(c, func(string) (string, bool) { return first, true }); err != nil || value != first {
		t.Fatal("valid environment key rejected")
	}
}

func TestServerBDD_SQLitePathIsLiteralAndUnknownSchemaFailsClosed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state?mode=memory#part")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(filepath.Join(dir, "research.db")); err != nil {
		t.Fatal("database path was interpreted as URI options")
	}
	bad := emptyData()
	bad.Schema = 99
	raw, _ := json.Marshal(bad)
	if _, err := s.db.Exec("UPDATE current_state SET data=? WHERE id=1", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(); err == nil {
		t.Fatal("unsupported schema read")
	}
	if err := s.Change(0, "", "", "", "", "", func(*Data) error {
		t.Fatal("unsupported schema mutated")
		return nil
	}); err == nil {
		t.Fatal("unsupported schema accepted")
	}
}

func TestServerBDD_CoddyCandidateMustMatchRecordedDigest(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	o.Coddy = &coddy.Config{APIURL: "http://127.0.0.1:1", AllowLoopbackHTTP: true, APIVersion: "2026-03-10",
		Repository: "org/lab", BotLogin: "coddy-bot", RequesterLogin: "research-owner", BaseBranch: "main",
		TokenEnv: "GITHUB_TOKEN", CoddyRevision: coddy.SourceRevision, CoddyPatchset: "review-loop-v1",
		TimeoutSeconds: 2, MaxPages: 2}
	s := serviceFor(t, o)
	v := studyFor(t, s)
	v = act(t, s, Action{Type: "TASK", Target: v.Studies[0].Goal, Title: "Checker", Kind: "proof", Text: "Check cases"})
	if err := s.PrepareCoddy(PrepareCoddy{ExpectedRevision: v.Revision, RequestID: "prepare-digest-001",
		TaskID: v.Tasks[0].ID, SourceCommit: strings.Repeat("1", 40), Acceptance: []string{"Check"}}); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, s).Delegations[0].ID
	record, err := s.CoddyLocal(id)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"status":"candidate"}`)
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	record.Candidates = []string{digest}
	dir := filepath.Join(o.Config.DataDir, "coddy")
	writeJSON(t, filepath.Join(dir, id+".json"), record)
	path := filepath.Join(dir, "candidate-"+digest+".json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if value, err := s.CoddyCandidate(id, digest); err != nil || string(value) != string(data) {
		t.Fatal("valid candidate rejected", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CoddyCandidate(id, digest); err == nil {
		t.Fatal("tampered candidate accepted")
	}
	if _, err := s.CoddyCandidate(id, strings.Repeat("0", 64)); err == nil {
		t.Fatal("foreign candidate accepted")
	}
}
