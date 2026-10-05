package researchweb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretsBDD_PrivateFileOnly(t *testing.T) {
	for _, scenario := range []string{"valid", "public-file", "public-parent", "web-root", "symlink", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			dir, web := t.TempDir(), t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(web, 0700); err != nil {
				t.Fatal(err)
			}
			if scenario == "web-root" {
				dir = web
			}
			path := filepath.Join(dir, "secrets.json")
			data := []byte("{\"TEST_TOKEN\":\"private-test-value\"}")
			if scenario == "invalid" {
				data = []byte("{private-test-value")
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "public-file" {
				_ = os.Chmod(path, 0644)
			}
			if scenario == "public-parent" {
				_ = os.Chmod(dir, 0755)
			}
			if scenario == "symlink" {
				link := filepath.Join(dir, "link")
				if err := os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				path = link
			}
			values, err := readPrivateSecrets(path, web)
			if scenario == "valid" {
				if err != nil || values["TEST_TOKEN"] != "private-test-value" {
					t.Fatal("private secret unavailable")
				}
			} else if err == nil || strings.Contains(err.Error(), "private-test-value") {
				t.Fatal("unsafe secret handling")
			}
		})
	}
}
