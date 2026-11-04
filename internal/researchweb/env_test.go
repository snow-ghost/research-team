package researchweb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOptionsWithLookupPreservesEnvironmentAfterSecrets(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "web"), 0700); err != nil {
		t.Fatal(err)
	}
	secret := []byte(`{"SECRET_KEY":"from-secret"}`)
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), secret, 0600); err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"listen":"127.0.0.1:4187","data_dir":"state","web_dir":"web","secret_file":"secrets.json","max_parallel":1}`)
	path := filepath.Join(dir, "server.json")
	if err := os.WriteFile(path, config, 0600); err != nil {
		t.Fatal(err)
	}
	lookup := func(key string) (string, bool) {
		if key == "ENV_KEY" {
			return "from-env", true
		}
		return "", false
	}
	options, err := LoadOptionsWithLookup(path, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := options.Lookup("ENV_KEY"); !ok || value != "from-env" {
		t.Fatal("environment fallback was lost")
	}
	if value, ok := options.Lookup("SECRET_KEY"); !ok || value != "from-secret" {
		t.Fatal("secret value was not preserved")
	}
}
