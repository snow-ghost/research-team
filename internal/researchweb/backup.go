package researchweb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type BackupManifest struct {
	Version         int               `json:"version"`
	Revision        int               `json:"revision"`
	CreatedAt       time.Time         `json:"created_at"`
	Files           map[string]string `json:"files"`
	Tables          map[string]string `json:"tables"`
	DatabaseSHA256  string            `json:"database_sha256"`
	RestoreVerified bool              `json:"restore_verified"`
}

// The runner executes pg_dump/pg_restore in the operator-selected PostgreSQL container.
type BackupRunner func(context.Context, io.Reader, io.Writer, ...string) error

func BackupPostgres(ctx context.Context, c Config, lookup func(string) (string, bool), destination string, run BackupRunner) (BackupManifest, error) {
	m := BackupManifest{Version: 1, CreatedAt: time.Now().UTC()}
	if c.Database.Driver != "postgres" || run == nil {
		return m, errors.New("backup requires PostgreSQL and a container runner")
	}
	destination, err := filepath.Abs(destination)
	if err != nil {
		return m, err
	}
	stateDir, err := filepath.Abs(c.DataDir)
	if err != nil {
		return m, err
	}
	if rel, err := filepath.Rel(stateDir, destination); err != nil || rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
		return m, errors.New("backup destination must be outside the state directory")
	}
	store, err := OpenDatabase(c, lookup)
	if err != nil {
		return m, err
	}
	defer store.Close()
	v, err := store.Read()
	if err != nil {
		return m, err
	}
	for _, a := range v.Attempts {
		if active(a.Status) {
			return m, errors.New("stop active attempts before backup")
		}
	}
	for _, v := range v.Verifications {
		if v.Status == "queued" || v.Status == "running" {
			return m, errors.New("finish queued verifications before backup")
		}
	}
	for _, l := range v.Library {
		if l.Status == "queued" || l.Status == "running" {
			return m, errors.New("finish queued library builds before backup")
		}
	}
	m.Revision = v.Revision
	if err = os.Mkdir(destination, 0700); err != nil {
		return m, errors.New("backup destination must not exist")
	}
	// A partial directory has no completed manifest and must not be used for recovery.
	m.Files, err = backupFiles(stateDir, filepath.Join(destination, "state"))
	if err != nil {
		return m, err
	}
	m.Tables, err = backupTables(ctx, store.pg.conn)
	if err != nil {
		return m, err
	}
	dsn, _ := lookup(c.Database.DSNEnv)
	dbConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		return m, errors.New("invalid database configuration")
	}
	dump, err := os.OpenFile(filepath.Join(destination, "database.dump"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return m, err
	}
	err = run(ctx, nil, dump, "pg_dump", "-U", dbConfig.User, "-d", dbConfig.Database, "-Fc")
	closeErr := dump.Close()
	if err != nil {
		return m, err
	}
	if closeErr != nil {
		return m, closeErr
	}
	m.DatabaseSHA256, err = fileSHA(filepath.Join(destination, "database.dump"))
	if err != nil {
		return m, err
	}
	check, err := backupTables(ctx, store.pg.conn)
	if err != nil || !reflect.DeepEqual(check, m.Tables) {
		return m, errors.New("database changed during backup")
	}
	files, err := backupFiles(stateDir, "")
	if err != nil || !reflect.DeepEqual(files, m.Files) {
		return m, errors.New("state files changed during backup")
	}
	if err = verifyBackupRestore(ctx, m, destination, dbConfig, run); err != nil {
		return m, err
	}
	m.RestoreVerified = true
	data, _ := json.MarshalIndent(m, "", "  ")
	if err = os.WriteFile(filepath.Join(destination, "manifest.json"), append(data, '\n'), 0600); err != nil {
		return m, err
	}
	return m, nil
}

type backupQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func backupTables(ctx context.Context, db backupQuery) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
	if err != nil {
		return nil, errors.New("cannot list backup tables")
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, name := range names {
		query := `SELECT count(*)::text || ':' || md5(COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text)::text,'[]')) FROM "` + strings.ReplaceAll(name, `"`, `""`) + `" r`
		var digest string
		if err = db.QueryRowContext(ctx, query).Scan(&digest); err != nil {
			return nil, errors.New("cannot compare backup table contents")
		}
		out[name] = digest
	}
	return out, nil
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func backupFiles(source, destination string) (map[string]string, error) {
	out := map[string]string{}
	var total int64
	if destination != "" {
		if err := os.Mkdir(destination, 0700); err != nil {
			return nil, err
		}
	}
	err := filepath.WalkDir(source, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, name)
		if err != nil || rel == "." {
			return err
		}
		if rel == ".server-lock" || rel == "access-token" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!d.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("state backup rejects symlinks and special files")
		}
		if d.IsDir() {
			if destination != "" {
				return os.Mkdir(filepath.Join(destination, rel), 0700)
			}
			return nil
		}
		total += info.Size()
		if info.Size() > 512<<20 || total > 2<<30 {
			return errors.New("state backup exceeds the size bound")
		}
		if destination != "" {
			in, err := os.Open(name)
			if err != nil {
				return err
			}
			to, err := os.OpenFile(filepath.Join(destination, rel), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				in.Close()
				return err
			}
			_, err = io.Copy(to, in)
			in.Close()
			closeErr := to.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		digest, err := fileSHA(name)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = digest
		if destination != "" {
			copied, err := fileSHA(filepath.Join(destination, rel))
			if err != nil || copied != digest {
				return errors.New("copied state artifact differs")
			}
		}
		return nil
	})
	return out, err
}

func verifyBackupRestore(ctx context.Context, m BackupManifest, directory string, config *pgx.ConnConfig, run BackupRunner) error {
	target := "research_restore_" + strings.ReplaceAll(identifier("drill"), "-", "") + "_test"
	if err := run(ctx, nil, io.Discard, "createdb", "-U", "postgres", "-O", config.User, target); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = run(cleanup, nil, io.Discard, "dropdb", "-U", "postgres", target)
	}()
	f, err := os.Open(filepath.Join(directory, "database.dump"))
	if err != nil {
		return err
	}
	err = run(ctx, f, io.Discard, "pg_restore", "-U", "postgres", "--role="+config.User, "--no-owner", "--exit-on-error", "-d", target)
	f.Close()
	if err != nil {
		return err
	}
	c := config.Copy()
	c.Database = target
	// pgx registers the parsed configuration without exposing credentials in arguments.
	registered := stdlib.RegisterConnConfig(c)
	defer stdlib.UnregisterConnConfig(registered)
	db, err := sql.Open("pgx", registered)
	if err != nil {
		return errors.New("restore database unavailable")
	}
	defer db.Close()
	actual, err := backupTables(ctx, db)
	if err != nil || !reflect.DeepEqual(actual, m.Tables) {
		return errors.New("restored database contents differ")
	}
	clone, err := os.MkdirTemp("", "research-state-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(clone)
	files, err := backupFiles(filepath.Join(directory, "state"), filepath.Join(clone, "state"))
	if err != nil || !reflect.DeepEqual(files, m.Files) {
		return errors.New("restored state artifacts differ")
	}
	cloned, err := backupFiles(filepath.Join(clone, "state"), "")
	if err != nil || !reflect.DeepEqual(cloned, m.Files) {
		return fmt.Errorf("restored state inventory differs")
	}
	return nil
}
