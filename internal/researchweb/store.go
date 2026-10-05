package researchweb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	_ "modernc.org/sqlite"
)

const maxState = 16 << 20

var requestPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,100}$`)

type Store struct {
	pg      *postgresStore
	db      *sql.DB
	mu      sync.Mutex
	release func()
}

func OpenStore(dir string) (*Store, error) {
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
	databasePath := filepath.Join(dir, "research.db")
	if info, err := os.Lstat(databasePath); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			release()
			return nil, errors.New("database file must be private and regular")
		}
	} else if !os.IsNotExist(err) {
		release()
		return nil, err
	} else {
		// Permissions must be private even if initialization is interrupted.
		file, err := os.OpenFile(databasePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			release()
			return nil, err
		}
		if err = file.Close(); err != nil {
			release()
			return nil, err
		}
	}
	absolutePath, err := filepath.Abs(databasePath)
	if err != nil {
		release()
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: absolutePath}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		release()
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, release: release}
	_, err = db.Exec(`PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
		PRAGMA max_page_count=65536;
		CREATE TABLE IF NOT EXISTS current_state(id INTEGER PRIMARY KEY CHECK(id=1), data BLOB NOT NULL);
		CREATE TABLE IF NOT EXISTS history(revision INTEGER PRIMARY KEY, metadata BLOB NOT NULL, snapshot BLOB NOT NULL);
		CREATE TABLE IF NOT EXISTS commands(id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL);`)
	if err != nil {
		s.Close()
		return nil, err
	}
	if err = os.Chmod(databasePath, 0600); err != nil {
		s.Close()
		return nil, err
	}
	data, _ := json.Marshal(emptyData())
	_, err = db.Exec("INSERT OR IGNORE INTO current_state(id,data) VALUES(1,?)", data)
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error {
	if s.pg != nil {
		return s.pg.Close()
	}
	err := s.db.Close()
	s.release()
	return err
}
func (s *Store) Read() (View, error) {
	if s.pg != nil {
		return s.pg.Read()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var data []byte
	if err := s.db.QueryRow("SELECT data FROM current_state WHERE id=1").Scan(&data); err != nil {
		return View{}, err
	}
	var view View
	if err := json.Unmarshal(data, &view.Data); err != nil {
		return view, err
	}
	if view.Schema != 1 || view.Revision < 1 {
		return view, errors.New("unsupported state schema or revision")
	}
	view.History = []History{}
	rows, err := s.db.Query("SELECT metadata FROM history ORDER BY revision DESC LIMIT 100")
	if err != nil {
		return view, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var h History
		if err = rows.Scan(&raw); err != nil {
			return view, err
		}
		if err = json.Unmarshal(raw, &h); err != nil {
			return view, err
		}
		view.History = append(view.History, h)
	}
	for i, j := 0, len(view.History)-1; i < j; i, j = i+1, j-1 {
		view.History[i], view.History[j] = view.History[j], view.History[i]
	}
	return view, rows.Err()
}
func (s *Store) Snapshot(revision int) (Data, error) {
	if s.pg != nil {
		return s.pg.Snapshot(revision)
	}
	var data []byte
	err := s.db.QueryRow("SELECT snapshot FROM history WHERE revision=?", revision).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) && revision == 1 {
		return emptyData(), nil
	}
	var snapshot Data
	if err == nil {
		err = json.Unmarshal(data, &snapshot)
	}
	return snapshot, err
}

// State, its historical snapshot and command deduplication commit together.
func (s *Store) Change(expected int, requestID, fingerprint, label, target, actor string, mutate func(*Data) error) error {
	change := mutate
	mutate = func(d *Data) error {
		if err := change(d); err != nil {
			return err
		}
		refreshEvidence(d)
		return nil
	}
	if s.pg != nil {
		return s.pg.Change(expected, requestID, fingerprint, label, target, actor, mutate)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if requestID != "" {
		if !requestPattern.MatchString(requestID) {
			return RuleError("Нужен идентификатор команды.")
		}
		var previous string
		err = tx.QueryRow("SELECT fingerprint FROM commands WHERE id=?", requestID).Scan(&previous)
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
	var raw []byte
	if err = tx.QueryRow("SELECT data FROM current_state WHERE id=1").Scan(&raw); err != nil {
		return err
	}
	var data Data
	if err = json.Unmarshal(raw, &data); err != nil {
		return err
	}
	if data.Schema != 1 || data.Revision < 1 {
		return errors.New("unsupported state schema or revision")
	}
	if expected != 0 && expected != data.Revision {
		return ErrConflict
	}
	if err = mutate(&data); err != nil {
		return err
	}
	if err := validateState(data); err != nil {
		return err
	}
	if len(data.Entities) > 2000 || len(data.Tasks) > 2000 || len(data.Questions) > 2000 || len(data.Attempts) > 100 || len(data.Findings) > 2000 || len(data.Cycles) > 100 {
		return ErrLimit
	}
	data.Revision++
	raw, err = json.Marshal(data)
	if err != nil {
		return err
	}
	if len(raw) > maxState {
		return ErrLimit
	}
	h, _ := json.Marshal(eventFor(&data, label, target, "", actor))
	if _, err = tx.Exec("UPDATE current_state SET data=? WHERE id=1", raw); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO history(revision,metadata,snapshot) VALUES(?,?,?)", data.Revision, h, raw); err != nil {
		return err
	}
	if requestID != "" {
		if _, err = tx.Exec("INSERT INTO commands(id,fingerprint) VALUES(?,?)", requestID, fingerprint); err != nil {
			return err
		}
	}
	return tx.Commit()
}
