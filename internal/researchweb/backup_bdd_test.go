package researchweb

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

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
