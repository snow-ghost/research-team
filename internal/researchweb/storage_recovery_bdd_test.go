//go:build linux

package researchweb

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStorageRecoveryBDD_ReacquiresOwnershipPausesAndKeepsCommandLedger(t *testing.T) {
	dsn := postgresDSN(t)
	s, err := OpenPostgres(filepath.Join(t.TempDir(), "state"), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mutations := 0
	if err := s.Change(1, "command-recovery-001", "same-payload", "Fixture", "", "test", func(d *Data) error {
		mutations++
		if err := applyAction(d, Action{Type: "CREATE_STUDY", Title: "Recovery", Statement: "P", Assumptions: "None"}); err != nil {
			return err
		}
		goal := d.Studies[0].Goal
		d.addTask(goal, "Long task", "proof", "P")
		d.Attempts = append(d.Attempts, Attempt{ID: identifier("run"), TaskID: d.Tasks[0].ID, Target: goal, TargetRevision: 1, Profile: "reader", Workspace: "source", Status: "running", RemoteOutcome: "unknown", CreatedAt: time.Now().UTC()})
		d.Tasks[0].Attempt = d.Attempts[0].ID
		d.Teams = append(d.Teams, ResearchTeam{ID: identifier("team"), Study: d.Studies[0].ID, Goal: goal, Status: "running", Stage: "exploring", MaxAttempts: 4, UsedAttempts: 1, CreatedAt: time.Now().UTC()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	s.pg.mu.Lock()
	err = s.pg.conn.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&pid)
	s.pg.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatal(err)
	}
	after, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !after.Paused || after.Revision != before.Revision+1 || after.Attempts[0].Status != "interrupted" || after.Attempts[0].RemoteOutcome != "unknown" || after.Teams[0].Status != "interrupted" {
		t.Fatal("unsafe recovery", after)
	}
	if err := s.Change(1, "command-recovery-001", "same-payload", "Repeat", "", "test", func(*Data) error { mutations++; return nil }); err != nil {
		t.Fatal("ledger lost", err)
	}
	if mutations != 1 {
		t.Fatal("mutation replayed")
	}
	if err := s.Change(before.Revision, "command-recovery-002", "new", "Stale", "", "test", func(*Data) error { return nil }); !errors.Is(err, ErrConflict) {
		t.Fatal("old snapshot remained writable", err)
	}
	h := s.Health()
	if !h.Available || h.Generation != 2 || h.LastFailure != "connection_lost" || h.RecoveredAt == nil {
		t.Fatal(h)
	}
	history, err := s.Snapshot(before.Revision)
	if err != nil || history.Paused {
		t.Fatal("historical snapshot overwritten", err)
	}
}

func TestStorageRecoveryBDD_DifferentOwnerBlocksRecovery(t *testing.T) {
	dsn := postgresDSN(t)
	s, err := OpenPostgres(filepath.Join(t.TempDir(), "state"), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var pid int
	if err := s.pg.conn.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatal(err)
	}
	owner, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var locked bool
	if err := owner.QueryRowContext(context.Background(), "SELECT pg_try_advisory_lock($1)", postgresOwnerLock).Scan(&locked); err != nil || !locked {
		t.Fatal(err)
	}
	if _, err := s.Read(); err == nil {
		t.Fatal("second coordinator bypassed ownership")
	}
	h := s.Health()
	if h.Available || h.LastFailure != "ownership_unavailable" {
		t.Fatal(h)
	}
	if _, err := owner.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", postgresOwnerLock); err != nil {
		t.Fatal(err)
	}
	v, err := s.Read()
	if err != nil || !v.Paused {
		t.Fatal("recovery after owner release failed", err)
	}
}
