package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCredentialImportBDD_SelectedProviderAndMatchingHubOnly(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong-hub", "inline", "unknown-endpoint", "duplicate", "path-traversal"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, "providers", "chosen")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			hub := "https://hub.neuraldeep.ru"
			if scenario == "wrong-hub" {
				hub = "https://unexpected.invalid"
			}
			if err := os.WriteFile(filepath.Join(dir, "neuraldeep-auth.json"),
				[]byte("{\"api_key\":\"private-test-token\",\"hub\":\""+hub+"\"}"), 0600); err != nil {
				t.Fatal(err)
			}
			var c localConfig
			if yaml.Unmarshal([]byte("agent:\n  model: chosen/test-model\nproviders:\n  - name: chosen\n    type: neuraldeep\n"), &c) != nil {
				t.Fatal("fixture")
			}
			switch scenario {
			case "inline":
				c.Providers[0].Key = "private-inline-key"
			case "unknown-endpoint":
				c.Providers[0].Base = "https://unexpected.invalid"
			case "duplicate":
				c.Providers = append(c.Providers, c.Providers[0])
			case "path-traversal":
				c.Agent.Model = "../test-model"
			}
			key, endpoint, model, err := selectedCredential(c, home)
			if scenario == "valid" {
				if err != nil || key != "private-test-token" || endpoint != "https://api.neuraldeep.ru/v1" || model != "test-model" {
					t.Fatal("selected credential unavailable")
				}
			} else if err == nil || key != "" || strings.Contains(err.Error(), "private-") {
				t.Fatal("unsafe credential import")
			}
		})
	}
}
