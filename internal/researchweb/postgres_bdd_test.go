//go:build linux

package researchweb

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func postgresDSN(t *testing.T) string {
	t.Helper()
	value := os.Getenv("RESEARCH_TEST_POSTGRES_DSN")
	if value == "" {
		t.Skip("RESEARCH_TEST_POSTGRES_DSN is unset")
	}
	u, err := url.Parse(value)
	if err != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("use a dedicated PostgreSQL database ending in _test")
	}
	db, err := sql.Open("pgx", value)
	if err != nil {
		t.Fatal("test database configuration invalid")
	}
	t.Cleanup(func() { db.Close() })
	schema := strings.ReplaceAll(identifier("bdd"), "-", "_")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = db.Exec("CREATE SCHEMA " + quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP SCHEMA " + quoted + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
func pgServiceFor(t *testing.T, base string) *Service {
	t.Helper()
	dsn := postgresDSN(t)
	o := optionsFor(t, base)
	o.Config.Database = DatabaseConfig{Driver: "postgres", DSNEnv: "TEST_DB"}
	lookup := o.Lookup
	o.Lookup = func(key string) (string, bool) {
		if key == "TEST_DB" {
			return dsn, true
		}
		return lookup(key)
	}
	return serviceFor(t, o)
}

func TestPostgresBDD_RelationsVersionsTransactionsAndOwnership(t *testing.T) {
	dsn := postgresDSN(t)
	dir := filepath.Join(t.TempDir(), "state")
	store, err := OpenPostgres(dir, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := OpenPostgres(filepath.Join(t.TempDir(), "second"), dsn); err == nil {
		t.Fatal("second coordinator acquired ownership")
	}
	a := Action{Type: "CREATE_STUDY", Title: "Study", Statement: "Claim", Assumptions: "Conditions"}
	change := func(d *Data) error { return applyAction(d, a) }
	if err = store.Change(1, "command-pg-001", hash(a), "Create", "", "operator", change); err != nil {
		t.Fatal(err)
	}
	if err = store.Change(1, "command-pg-001", hash(a), "Create", "", "operator", change); err != nil {
		t.Fatal(err)
	}
	if err = store.Change(1, "command-pg-001", "different", "Create", "", "operator", change); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate payload accepted")
	}
	v, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	goal := v.Studies[0].Goal
	if err = store.Change(v.Revision, "command-pg-002", "split", "Split", goal, "operator", func(d *Data) error {
		return applyAction(d, Action{Type: "SPLIT", Target: goal, Parts: []string{"Case one", "Case two"}})
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = store.pg.conn.QueryRowContext(context.Background(), "SELECT count(*) FROM dependencies WHERE source_id=$1", goal).Scan(&count); err != nil || count != 3 {
		t.Fatalf("relations: %d %v", count, err)
	}
	if err = store.pg.conn.QueryRowContext(context.Background(), "SELECT count(*) FROM entity_versions WHERE entity_id=$1", goal).Scan(&count); err != nil || count != 2 {
		t.Fatalf("versions: %d %v", count, err)
	}
	old, err := store.Snapshot(2)
	if err != nil || len(old.Entities) != 1 || len(old.Entities[0].Dependencies) != 0 {
		t.Fatal("historical version changed", err)
	}
	err = store.Change(3, "command-pg-bad", "broken", "Broken", goal, "operator", func(d *Data) error {
		d.Entities[0].Dependencies = append(d.Entities[0].Dependencies, "missing-entity")
		return nil
	})
	if err == nil {
		t.Fatal("foreign key allowed missing dependency")
	}
	v, _ = store.Read()
	if v.Revision != 3 || len(v.Entities[0].Dependencies) != 3 {
		t.Fatal("constraint failure was not atomic")
	}
	var success atomic.Int32
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			err := store.Change(3, identifier("cmd"), "pause", "Pause", "", "operator", func(d *Data) error { d.Paused = true; return nil })
			if err == nil {
				success.Add(1)
			} else if !errors.Is(err, ErrConflict) {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if success.Load() != 1 {
		t.Fatal("stale writes committed")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenPostgres(dir, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	v, err = reopened.Read()
	if err != nil || v.Revision != 4 || !v.Paused {
		t.Fatal("reopen lost state", err)
	}
}

func TestPostgresBDD_ImportPreservesHistoryAndCommandIdentity(t *testing.T) {
	dsn := postgresDSN(t)
	dir := filepath.Join(t.TempDir(), "state")
	source, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := Action{Type: "CREATE_STUDY", Title: "Imported", Statement: "Claim", Assumptions: "Conditions"}
	if err = source.Change(1, "import-command-001", hash(a), "Create", "", "operator", func(d *Data) error { return applyAction(d, a) }); err != nil {
		t.Fatal(err)
	}
	before, _ := source.Read()
	source.Close()
	dest, err := OpenPostgres(dir, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer dest.Close()
	if err = dest.RequireImportedSQLite(dir); err == nil {
		t.Fatal("existing SQLite silently ignored")
	}
	if err = dest.ImportSQLite(); err != nil {
		t.Fatal(err)
	}
	if err = dest.RequireImportedSQLite(dir); err != nil {
		t.Fatal("imported source rejected", err)
	}
	after, err := dest.Read()
	if err != nil || after.Revision != before.Revision || after.Studies[0].Title != "Imported" {
		t.Fatal("import lost current state", err)
	}
	if past, err := dest.Snapshot(2); err != nil || past.Studies[0].ID != before.Studies[0].ID {
		t.Fatal("import lost history", err)
	}
	if err = dest.Change(1, "import-command-001", hash(a), "", "", "", func(*Data) error { t.Fatal("command repeated after import"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err = dest.ImportSQLite(); err == nil {
		t.Fatal("nonempty destination overwritten")
	}
	if _, err = os.Stat(filepath.Join(dir, "research.db")); err != nil {
		t.Fatal("source was removed")
	}
}

func TestPostgresBDD_UnknownOrChangedMigrationsAreRejected(t *testing.T) {
	dsn := postgresDSN(t)
	dir := filepath.Join(t.TempDir(), "state")
	s, err := OpenPostgres(dir, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pg.conn.ExecContext(context.Background(), "UPDATE schema_migrations SET checksum='changed' WHERE version=1"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if reopened, err := OpenPostgres(dir, dsn); err == nil {
		reopened.Close()
		t.Fatal("modified migration accepted")
	}
}

func waitCycle(t *testing.T, s *Service, id, status string) View {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		v := stateOf(t, s)
		if c := v.cycle(id); c != nil && c.Status == status {
			return v
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("cycle did not reach %s: %+v", status, stateOf(t, s).Cycles)
	return View{}
}

func TestPostgresBDD_ImportedCandidateKeepsArtifactsAndCanBeReviewed(t *testing.T) {
	dsn := postgresDSN(t)
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); completeModel(w, "Imported candidate.") }))
	defer model.Close()
	o := optionsFor(t, model.URL+"/v1")
	source := serviceFor(t, o)
	v := studyFor(t, source)
	goal := v.Studies[0].Goal
	v = act(t, source, Action{Type: "TASK", Target: goal, Title: "Proof", Kind: "proof", Text: "Prove the claim"})
	if err := source.Start(RunRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), TaskID: v.Tasks[0].ID, Profile: "reader", Workspace: "source", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	id := stateOf(t, source).Attempts[0].ID
	waitAttempt(t, source, id, func(a Attempt) bool { return a.Status == "candidate" })
	source.Close()
	dest, err := OpenPostgres(o.Config.DataDir, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err = dest.ImportSQLite(); err != nil {
		dest.Close()
		t.Fatal(err)
	}
	dest.Close()
	o.Config.Database = DatabaseConfig{Driver: "postgres", DSNEnv: "TEST_DB"}
	lookup := o.Lookup
	o.Lookup = func(key string) (string, bool) {
		if key == "TEST_DB" {
			return dsn, true
		}
		return lookup(key)
	}
	s := serviceFor(t, o)
	if result, err := s.Result(id); err != nil || result.Candidate != "Imported candidate." {
		t.Fatal("artifact reference lost", err)
	}
	act(t, s, Action{Type: "ATTACH_PROOF", Target: goal, Attempt: id})
	v = act(t, s, Action{Type: "REVIEW", Target: goal, Decision: "accept", Text: "Проверено после переноса."})
	if v.entity(goal).Status != "accepted" || calls.Load() != 1 {
		t.Fatal("import replayed work or prevented review")
	}
	s.Store.pg.mu.Lock()
	var count int
	err = s.Store.pg.conn.QueryRowContext(context.Background(), "SELECT count(*) FROM review_decisions WHERE entity_id=$1 AND decision='accept'", goal).Scan(&count)
	s.Store.pg.mu.Unlock()
	if err != nil || count != 1 {
		t.Fatal("decision ledger missing", err)
	}
}
