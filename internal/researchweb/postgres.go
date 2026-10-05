package researchweb

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const postgresOwnerLock int64 = 731946218057

type postgresStore struct {
	dir     string
	mu      sync.Mutex
	db      *sql.DB
	conn    *sql.Conn
	release func()
}

type collectionSpec struct{ field, table string }

var collections = []collectionSpec{
	{"studies", "studies"}, {"entities", "entities"}, {"workLinks", "work_links"},
	{"tasks", "tasks"}, {"questions", "questions"}, {"findings", "findings"},
	{"applications", "applications"}, {"attempts", "attempts"}, {"delegations", "delegations"},
	{"cycles", "research_cycles"},
	{"verifications", "proof_verifications"},
	{"teams", "research_teams"},
	{"results", "research_results"}, {"proposals", "decomposition_proposals"}, {"library", "lemma_library"},
	{"bindings", "study_connectors"},
	{"messages", "study_messages"}, {"telegram_offsets", "telegram_offsets"},
}

type storedObject struct {
	id       string
	position int
	data     json.RawMessage
}

func OpenDatabase(config Config, lookup func(string) (string, bool)) (*Store, error) {
	if config.Database.Driver == "" || config.Database.Driver == "sqlite" {
		return OpenStore(config.DataDir)
	}
	if config.Database.Driver != "postgres" {
		return nil, errors.New("unsupported database driver")
	}
	dsn, ok := lookup(config.Database.DSNEnv)
	if !ok || dsn == "" {
		return nil, errors.New("database connection environment value unavailable")
	}
	return OpenPostgres(config.DataDir, dsn)
}

func OpenPostgres(dir, dsn string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("state directory must be private (0700), not a symlink")
	}
	release, err := lockStore(dir)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		release()
		return nil, errors.New("invalid postgres connection configuration")
	}
	db.SetMaxOpenConns(1)
	p := &postgresStore{db: db, release: release, dir: dir}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p.conn, err = db.Conn(ctx)
	if err != nil {
		p.Close()
		return nil, errors.New("postgres connection unavailable")
	}
	var owned bool
	err = p.conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", postgresOwnerLock).Scan(&owned)
	if err != nil || !owned {
		p.Close()
		return nil, errors.New("postgres workspace unavailable or already owned by another server")
	}
	if err = p.migrate(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Store{pg: p}, nil
}

func (p *postgresStore) Close() error {
	if p.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, _ = p.conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", postgresOwnerLock)
		cancel()
		_ = p.conn.Close()
	}
	err := p.db.Close()
	p.release()
	return err
}

func (p *postgresStore) migrate(ctx context.Context) error {
	if _, err := p.conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations(version integer PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	files, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	var latest, count, first int
	if err = p.conn.QueryRowContext(ctx, "SELECT COALESCE(max(version),0),count(*),COALESCE(min(version),1) FROM schema_migrations").Scan(&latest, &count, &first); err != nil {
		return err
	}
	if first < 1 || count != latest {
		return errors.New("database migration history is incomplete")
	}
	if latest > len(files) {
		return errors.New("database schema is newer than this server")
	}
	for i, file := range files {
		body, err := migrationFiles.ReadFile(filepath.Join("migrations", file.Name()))
		if err != nil {
			return err
		}
		checksum := hash(string(body))
		var saved string
		err = p.conn.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version=$1", i+1).Scan(&saved)
		if err == nil {
			if saved != checksum {
				return errors.New("migration checksum mismatch")
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		tx, err := p.conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)", i+1, checksum)
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func stateParts(d Data) (map[string]json.RawMessage, map[string][]storedObject, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, nil, err
	}
	var metadata map[string]json.RawMessage
	if err = json.Unmarshal(raw, &metadata); err != nil {
		return nil, nil, err
	}
	objects := map[string][]storedObject{}
	for _, c := range collections {
		var items []json.RawMessage
		if value, exists := metadata[c.field]; exists {
			if err = json.Unmarshal(value, &items); err != nil {
				return nil, nil, err
			}
		}
		delete(metadata, c.field)
		seen := map[string]bool{}
		for pos, item := range items {
			var identity struct {
				ID string `json:"id"`
			}
			id := ""
			if c.field == "workLinks" {
				id = hash(json.RawMessage(item))
			} else {
				if err = json.Unmarshal(item, &identity); err != nil {
					return nil, nil, err
				}
				id = identity.ID
			}
			if id == "" || seen[id] {
				return nil, nil, errors.New("empty or duplicate object identity")
			}
			seen[id] = true
			objects[c.field] = append(objects[c.field], storedObject{id, pos, item})
		}
	}
	return metadata, objects, nil
}

func dataFromParts(metadata []byte, objects map[string][]json.RawMessage) (Data, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &fields); err != nil {
		return Data{}, err
	}
	for _, c := range collections {
		values := objects[c.field]
		if values == nil {
			values = []json.RawMessage{}
		}
		fields[c.field], _ = json.Marshal(values)
	}
	raw, _ := json.Marshal(fields)
	var d Data
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, err
	}
	if d.Schema != 1 || d.Revision < 1 {
		return d, errors.New("unsupported state schema or revision")
	}
	return d, nil
}

func readCurrent(ctx context.Context, tx *sql.Tx) (Data, error) {
	var meta []byte
	if err := tx.QueryRowContext(ctx, "SELECT metadata FROM workspace WHERE id=1").Scan(&meta); err != nil {
		return Data{}, err
	}
	objects := map[string][]json.RawMessage{}
	for _, c := range collections {
		rows, err := tx.QueryContext(ctx, "SELECT data FROM "+c.table+" ORDER BY position")
		if err != nil {
			return Data{}, err
		}
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return Data{}, err
			}
			objects[c.field] = append(objects[c.field], json.RawMessage(raw))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return Data{}, err
		}
	}
	return dataFromParts(meta, objects)
}

func (p *postgresStore) Read() (View, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := p.conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	d, err := readCurrent(ctx, tx)
	if err != nil {
		return View{}, err
	}
	v := View{Data: d, History: []History{}}
	rows, err := tx.QueryContext(ctx, "SELECT metadata FROM (SELECT revision,metadata FROM events ORDER BY revision DESC LIMIT 100) h ORDER BY revision")
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var raw []byte
		var h History
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal(raw, &h)
		}
		if err != nil {
			rows.Close()
			return v, err
		}
		v.History = append(v.History, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}

func (p *postgresStore) Snapshot(revision int) (Data, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if revision == 1 {
		return emptyData(), nil
	}
	var metadata []byte
	if err := p.conn.QueryRowContext(ctx, "SELECT state_metadata FROM events WHERE revision=$1", revision).Scan(&metadata); err != nil {
		return Data{}, err
	}
	// Filter tombstones only after choosing the last version of each object.
	rows, err := p.conn.QueryContext(ctx, `SELECT collection,data FROM
		(SELECT DISTINCT ON(collection,object_id) collection,object_id,position,data
		 FROM object_versions WHERE revision <= $1 ORDER BY collection,object_id,revision DESC) v
		 WHERE data IS NOT NULL ORDER BY collection,position`, revision)
	if err != nil {
		return Data{}, err
	}
	defer rows.Close()
	objects := map[string][]json.RawMessage{}
	for rows.Next() {
		var collection string
		var raw []byte
		if err = rows.Scan(&collection, &raw); err != nil {
			return Data{}, err
		}
		objects[collection] = append(objects[collection], json.RawMessage(raw))
	}
	if err = rows.Err(); err != nil {
		return Data{}, err
	}
	return dataFromParts(metadata, objects)
}

func validateState(d Data) error {
	if len(d.Results) > 200 || len(d.Proposals) > 200 || len(d.Library) > 200 {
		return ErrLimit
	}
	if d.Schema != 1 || d.Revision < 1 {
		return errors.New("unsupported state schema or revision")
	}
	if len(d.Entities) > 2000 || len(d.Tasks) > 2000 || len(d.Questions) > 2000 || len(d.Attempts) > 100 || len(d.Findings) > 2000 || len(d.Cycles) > 100 || len(d.Teams) > 100 || len(d.Verifications) > 200 || len(d.Bindings) > 200 || len(d.Messages) > 2000 {
		return ErrLimit
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(raw) > maxState {
		return ErrLimit
	}
	return nil
}

func (p *postgresStore) Change(expected int, requestID, fingerprint, label, target, actor string, mutate func(*Data) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := p.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM workspace WHERE id=1 FOR UPDATE").Scan(&current); err != nil {
		return err
	}
	if requestID != "" {
		if !requestPattern.MatchString(requestID) {
			return RuleError("Нужен идентификатор команды.")
		}
		var previous string
		err = tx.QueryRowContext(ctx, "SELECT fingerprint FROM commands WHERE id=$1", requestID).Scan(&previous)
		if err == nil {
			if previous != fingerprint {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if expected != 0 && expected != current {
		return ErrConflict
	}
	before, err := readCurrent(ctx, tx)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(before)
	var after Data
	if err = json.Unmarshal(raw, &after); err != nil {
		return err
	}
	if err = mutate(&after); err != nil {
		return err
	}
	after.Revision = current + 1
	if err = validateState(after); err != nil {
		return err
	}
	h := eventFor(&after, label, target, "", actor)
	if err = writeDelta(ctx, tx, before, after, h); err != nil {
		return err
	}
	if requestID != "" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO commands(id,fingerprint) VALUES($1,$2)", requestID, fingerprint); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func writeDelta(ctx context.Context, tx *sql.Tx, before, after Data, h History) error {
	_, previous, err := stateParts(before)
	if err != nil {
		return err
	}
	meta, next, err := stateParts(after)
	if err != nil {
		return err
	}
	metaJSON, _ := json.Marshal(meta)
	eventJSON, _ := json.Marshal(h)
	if _, err = tx.ExecContext(ctx, "INSERT INTO events(revision,metadata,state_metadata) VALUES($1,$2,$3)", after.Revision, string(eventJSON), string(metaJSON)); err != nil {
		return err
	}
	for _, c := range collections {
		old := map[string]storedObject{}
		for _, object := range previous[c.field] {
			old[object.id] = object
		}
		for _, object := range next[c.field] {
			was, existed := old[object.id]
			delete(old, object.id)
			if existed && was.position == object.position && bytes.Equal(was.data, object.data) {
				continue
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO "+c.table+"(id,position,data) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET position=excluded.position,data=excluded.data", object.id, object.position, string(object.data)); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO object_versions(collection,object_id,revision,position,data) VALUES($1,$2,$3,$4,$5)", c.field, object.id, after.Revision, object.position, string(object.data)); err != nil {
				return err
			}
			if c.field == "entities" {
				var e Entity
				if err = json.Unmarshal(object.data, &e); err != nil {
					return err
				}
				if _, err = tx.ExecContext(ctx, "DELETE FROM dependencies WHERE source_id=$1", e.ID); err != nil {
					return err
				}
				for i, dep := range e.Dependencies {
					var pin any
					if e.DependencyRevisions[dep] > 0 {
						pin = e.DependencyRevisions[dep]
					}
					if _, err = tx.ExecContext(ctx, "INSERT INTO dependencies(source_id,target_id,required_revision,position) VALUES($1,$2,$3,$4)", e.ID, dep, pin, i); err != nil {
						return err
					}
				}
			}
		}
		for id, object := range old {
			if c.field == "entities" {
				if _, err = tx.ExecContext(ctx, "DELETE FROM dependencies WHERE source_id=$1", id); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, "DELETE FROM "+c.table+" WHERE id=$1", id); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO object_versions(collection,object_id,revision,position,data) VALUES($1,$2,$3,$4,NULL)", c.field, id, after.Revision, object.position); err != nil {
				return err
			}
		}
	}
	if h.Label == actionLabel("REVIEW") {
		e := after.entity(h.Target)
		if e == nil {
			return errors.New("review target missing")
		}
		decision := "reject"
		if e.Status == "accepted" {
			decision = "accept"
		}
		var attempt any
		if e.ProofAttempt != "" {
			attempt = e.ProofAttempt
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO review_decisions(state_revision,entity_id,statement_revision,attempt_id,decision,actor,reason) VALUES($1,$2,$3,$4,$5,$6,$7)",
			after.Revision, e.ID, e.Revision, attempt, decision, e.ReviewedBy, e.ReviewReason); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE workspace SET revision=$1,metadata=$2 WHERE id=1", after.Revision, string(metaJSON))
	return err
}

func (s *Store) Backend() string {
	if s.pg != nil {
		return "postgres"
	}
	return "sqlite"
}

// Connection strings can contain passwords; never include them in API metadata.
func databaseSecret(config Config, lookup func(string) (string, bool)) string {
	if config.Database.DSNEnv == "" {
		return ""
	}
	value, _ := lookup(config.Database.DSNEnv)
	return strings.TrimSpace(value)
}
