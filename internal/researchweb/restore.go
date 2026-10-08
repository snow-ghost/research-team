package researchweb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"

	"github.com/jackc/pgx/v5"
)

func ValidateBackup(directory string) (BackupManifest, error) {
	var m BackupManifest
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return m, errors.New("backup directory must be private and not a symlink")
	}
	if err := ReadJSON(filepath.Join(directory, "manifest.json"), &m); err != nil {
		return m, err
	}
	if m.Version != 1 || !m.RestoreVerified || m.Revision < 1 || len(m.Tables) == 0 {
		return m, errors.New("backup has no verified completion manifest")
	}
	digest, err := fileSHA(filepath.Join(directory, "database.dump"))
	if err != nil || digest != m.DatabaseSHA256 {
		return m, errors.New("database backup integrity mismatch")
	}
	files, err := backupFiles(filepath.Join(directory, "state"), "")
	if err != nil || !reflect.DeepEqual(files, m.Files) {
		return m, errors.New("backup artifact integrity mismatch")
	}
	return m, nil
}

// Recovery creates a new database and state directory; the original is never overwritten.
func RestorePostgres(ctx context.Context, c Config, lookup func(string) (string, bool), backup string, run BackupRunner) (BackupManifest, error) {
	m, err := ValidateBackup(backup)
	if err != nil {
		return m, err
	}
	if c.Database.Driver != "postgres" || run == nil {
		return m, errors.New("restore requires PostgreSQL")
	}
	dsn, _ := lookup(c.Database.DSNEnv)
	dbConfig, err := pgx.ParseConfig(dsn)
	if err != nil || !regexp.MustCompile(`^research_restore_[a-z0-9_]+$`).MatchString(dbConfig.Database) {
		return m, errors.New("restore database must have a new research_restore_ name")
	}
	if _, err = os.Lstat(c.DataDir); !os.IsNotExist(err) {
		return m, errors.New("restore state directory must not exist")
	}
	if err = run(ctx, nil, io.Discard, "createdb", "-U", "postgres", "-O", dbConfig.User, dbConfig.Database); err != nil {
		return m, errors.New("restore requires a new database; existing data is not overwritten")
	}
	// On failure leave the new database for inspection. No automatic drop of operator data.
	f, err := os.Open(filepath.Join(backup, "database.dump"))
	if err != nil {
		return m, err
	}
	err = run(ctx, f, io.Discard, "pg_restore", "-U", "postgres", "--role="+dbConfig.User, "--no-owner", "--no-privileges", "--exit-on-error", "-d", dbConfig.Database)
	f.Close()
	if err != nil {
		return m, errors.New("restore failed; inspect the new isolated database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return m, errors.New("restore connection unavailable")
	}
	defer db.Close()
	tables, err := backupTables(ctx, db)
	if err != nil {
		return m, errors.New("restored database verification connection or query unavailable")
	}
	if !reflect.DeepEqual(tables, m.Tables) {
		return m, errors.New("restored database differs from manifest")
	}
	if err = os.MkdirAll(filepath.Dir(c.DataDir), 0700); err != nil {
		return m, err
	}
	files, err := backupFiles(filepath.Join(backup, "state"), c.DataDir)
	if err != nil || !reflect.DeepEqual(files, m.Files) {
		return m, errors.New("restored artifacts differ from manifest")
	}
	// Recovery starts paused. This is a new journal command, not a rewrite of historical evidence.
	store, err := OpenDatabase(c, lookup)
	if err != nil {
		return m, err
	}
	defer store.Close()
	err = store.Change(0, "", "", "Восстановленный экземпляр приостановлен", "", "recovery", func(d *Data) error { d.Paused = true; return nil })
	if err != nil {
		return m, err
	}
	marker, _ := json.Marshal(map[string]any{"source_revision": m.Revision, "source_database_sha256": m.DatabaseSHA256, "historical_evidence_unchanged": true, "paused": true})
	if err = os.WriteFile(filepath.Join(c.DataDir, "recovery.json"), marker, 0600); err != nil {
		return m, err
	}
	return m, nil
}
