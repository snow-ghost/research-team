package researchweb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMaintenanceOptionsDoesNotRequireAgentConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.json")
	body := `{"listen":"127.0.0.1:4187","data_dir":"state","database":{"driver":"postgres","dsn_env":"RESEARCH_DATABASE_URL"},"token_env":"MISSING_TOKEN"}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	o, err := LoadMaintenanceOptions(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.Config.TokenEnv != "" || o.Config.DataDir != filepath.Join(dir, "state") {
		t.Fatalf("unexpected maintenance configuration: %+v", o.Config)
	}
}

func TestLoadMaintenanceOptionsRejectsRemoteAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(path, []byte(`{"listen":"0.0.0.0:4187","data_dir":"state"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMaintenanceOptions(path, nil); err == nil {
		t.Fatal("remote maintenance endpoint accepted")
	}
}
