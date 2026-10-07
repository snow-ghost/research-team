package researchweb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRestoreBDD_ManifestAndArtifactsMustMatch(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "state"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "state", "Goal.lean"), []byte("True"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "database.dump"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := backupFiles(filepath.Join(directory, "state"), "")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := fileSHA(filepath.Join(directory, "database.dump"))
	if err != nil {
		t.Fatal(err)
	}
	m := BackupManifest{Version: 1, Revision: 1, Files: files, Tables: map[string]string{"workspace": "fixture"}, DatabaseSHA256: digest, RestoreVerified: true}
	body, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateBackup(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "state", "Goal.lean"), []byte("False"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateBackup(directory); err == nil {
		t.Fatal("corrupted artifact accepted")
	}
}

func TestBackupBDD_PrivateFilesExactAndAccessKeyExcluded(t *testing.T) {
	source := t.TempDir()
	destination := filepath.Join(t.TempDir(), "copy")
	for name, body := range map[string]string{"Goal.lean": "def Statement : Prop := True", "access-token": "secret", ".server-lock": "lock"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := backupFiles(source, destination)
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	actual, err := backupFiles(destination, "")
	if err != nil || !reflect.DeepEqual(files, actual) {
		t.Fatal("restored inventory changed", err)
	}
	if _, err = os.Stat(filepath.Join(destination, "access-token")); !os.IsNotExist(err) {
		t.Fatal("access key copied")
	}
	if err = os.WriteFile(filepath.Join(destination, "Goal.lean"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	actual, err = backupFiles(destination, "")
	if err != nil || reflect.DeepEqual(files, actual) {
		t.Fatal("corruption invisible")
	}
	if err = os.Symlink(filepath.Join(source, "Goal.lean"), filepath.Join(source, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err = backupFiles(source, ""); err == nil {
		t.Fatal("symlink allowed")
	}
}
