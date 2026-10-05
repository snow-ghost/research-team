package researchweb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func (s *Store) RequireImportedSQLite(dir string) error {
	if s.pg == nil {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(dir, "research.db")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	s.pg.mu.Lock()
	defer s.pg.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var imported int
	if err := s.pg.conn.QueryRowContext(ctx, "SELECT count(*) FROM imports").Scan(&imported); err != nil {
		return err
	}
	if imported == 0 {
		return errors.New("existing SQLite workspace requires research-db -import-sqlite before starting PostgreSQL")
	}
	return nil
}

// The destination owns the same private directory, keeping artifact paths unchanged.
func (s *Store) ImportSQLite() error {
	if s.pg == nil {
		return errors.New("sqlite import requires postgres destination")
	}
	p := s.pg
	p.mu.Lock()
	defer p.mu.Unlock()
	path := filepath.Join(p.dir, "research.db")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("source database must be a private regular file")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro"}
	source, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer source.Close()
	source.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	read, err := source.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer read.Rollback()
	tx, err := p.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision, imported int
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM workspace WHERE id=1 FOR UPDATE").Scan(&revision); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM imports").Scan(&imported); err != nil {
		return err
	}
	if revision != 1 || imported > 0 {
		return errors.New("destination must be empty and not previously imported")
	}
	var raw []byte
	if err = read.QueryRowContext(ctx, "SELECT data FROM current_state WHERE id=1").Scan(&raw); err != nil {
		return err
	}
	var current Data
	if err = decodeJSON(raw, &current); err != nil {
		return err
	}
	if err = validateState(current); err != nil {
		return err
	}
	previous := emptyData()
	rows, err := read.QueryContext(ctx, "SELECT revision,metadata,snapshot FROM history ORDER BY revision")
	if err != nil {
		return err
	}
	for rows.Next() {
		var revision int
		var meta, snapshot []byte
		var d Data
		var h History
		if err = rows.Scan(&revision, &meta, &snapshot); err == nil {
			err = decodeJSON(snapshot, &d)
		}
		if err == nil {
			err = json.Unmarshal(meta, &h)
		}
		if err == nil {
			err = validateState(d)
		}
		if err != nil {
			rows.Close()
			return err
		}
		if revision != previous.Revision+1 || d.Revision != revision || h.ID != revision {
			rows.Close()
			return errors.New("source history is incomplete")
		}
		if err = writeDelta(ctx, tx, previous, d, h); err != nil {
			rows.Close()
			return err
		}
		previous = d
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	current.Cycles = append([]Cycle{}, current.Cycles...)
	previous.Cycles = append([]Cycle{}, previous.Cycles...)
	if hash(current) != hash(previous) {
		return errors.New("source current state does not match its history")
	}
	rows, err = read.QueryContext(ctx, "SELECT id,fingerprint FROM commands")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, fingerprint string
		if err = rows.Scan(&id, &fingerprint); err != nil {
			rows.Close()
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO commands(id,fingerprint) VALUES($1,$2)", id, fingerprint); err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO imports(id,source_digest,source_revision) VALUES(1,$1,$2)", hash(current), current.Revision); err != nil {
		return err
	}
	return tx.Commit()
}
