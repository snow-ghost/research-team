package researchweb

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Operational configuration and credentials are not copied into recovery kits.
func ExportPortableBackup(source, destination string) (BackupManifest, error) {
	m, err := ValidateBackup(source)
	if err != nil {
		return m, err
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return m, errors.New("portable destination must be new")
	}
	state := filepath.Join(destination, "state")
	if err = os.Mkdir(state, 0700); err != nil {
		return m, err
	}
	allowed := map[string]bool{"attempts": true, "verifications": true, "library": true, "inspections": true, "coddy": true}
	excluded := 0
	for name := range m.Files {
		if !allowed[strings.Split(name, "/")[0]] {
			excluded++
		}
	}
	for name := range allowed {
		path := filepath.Join(source, "state", name)
		if _, err = os.Stat(path); os.IsNotExist(err) {
			continue
		}
		if _, err = backupFiles(path, filepath.Join(state, name)); err != nil {
			return m, err
		}
	}
	m.Files, err = backupFiles(state, "")
	if err != nil {
		return m, err
	}
	from, err := os.Open(filepath.Join(source, "database.dump"))
	if err != nil {
		return m, err
	}
	defer from.Close()
	to, err := os.OpenFile(filepath.Join(destination, "database.dump"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return m, err
	}
	_, copyErr := io.Copy(to, from)
	closeErr := to.Close()
	if copyErr != nil || closeErr != nil {
		return m, errors.New("portable database copy failed")
	}
	body, _ := json.MarshalIndent(m, "", "  ")
	if err = atomicFile(filepath.Join(destination, "manifest.json"), body); err != nil {
		return m, err
	}
	metadata, _ := json.Marshal(map[string]any{"version": 1, "source_database_sha256": m.DatabaseSHA256, "excluded_operational_files": excluded, "credentials_configuration_copied": false, "research_artifacts_require_privacy_review": true, "requires_new_server_configuration": true})
	if err = atomicFile(filepath.Join(destination, "portable.json"), metadata); err != nil {
		return m, err
	}
	return ValidateBackup(destination)
}
