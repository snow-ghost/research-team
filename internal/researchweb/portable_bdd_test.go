package researchweb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPortableBDD_OperationalSecretsExcludedAndArtifactsPinned(t *testing.T) {
	source := t.TempDir()
	_ = os.Chmod(source, 0700)
	for name, text := range map[string]string{"state/coddy-launch/secrets.json": "private configuration", "state/attempts/a/result.json": "candidate", "database.dump": "fixture"} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := backupFiles(filepath.Join(source, "state"), "")
	digest, _ := fileSHA(filepath.Join(source, "database.dump"))
	m := BackupManifest{Version: 1, Revision: 1, RestoreVerified: true, Files: files, Tables: map[string]string{"workspace": "fixture"}, DatabaseSHA256: digest}
	body, _ := json.Marshal(m)
	_ = os.WriteFile(filepath.Join(source, "manifest.json"), body, 0600)
	destination := filepath.Join(t.TempDir(), "portable")
	portable, err := ExportPortableBackup(source, destination)
	if err != nil || len(portable.Files) != 1 || portable.Files["attempts/a/result.json"] != files["attempts/a/result.json"] {
		t.Fatal(portable, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "state", "coddy-launch")); !os.IsNotExist(err) {
		t.Fatal("operational configuration copied")
	}
	if _, err := ExportPortableBackup(source, destination); err == nil {
		t.Fatal("existing kit overwritten")
	}
	_ = os.WriteFile(filepath.Join(destination, "state", "attempts", "a", "result.json"), []byte("changed"), 0600)
	if _, err := ValidateBackup(destination); err == nil {
		t.Fatal("corrupted portable artifact accepted")
	}
}
