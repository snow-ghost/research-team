package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/researchweb"
)

func preflightOptions(t *testing.T) researchweb.Options {
	t.Helper()
	o, err := researchweb.LoadOptions("../../examples/server/postgres.json")
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestPreflightBDD_UnsupportedVersionDoesNotStartResearchOrRevealSecrets(t *testing.T) {
	o := preflightOptions(t)
	o.Lookup = func(string) (string, bool) { return "private-test-secret", true }
	calls := 0
	r := inspect(o, "/missing/coddy", func(string) (string, error) {
		calls++
		return "999.0.0", nil
	})
	if r.Ready || r.ModelCalled || calls != 1 {
		t.Fatal("unsupported candidate was reported ready")
	}
	found := false
	for _, c := range r.Checks {
		if strings.Contains(c.Detail, "private-test-secret") {
			t.Fatal("credential disclosed")
		}
		if strings.HasSuffix(c.ID, ":reviewed_version") && !c.Ready {
			found = true
		}
	}
	if !found {
		t.Fatal("missing version gate")
	}
}

func TestPreflightBDD_MissingBinaryAndExampleConfigurationFailClosed(t *testing.T) {
	o := preflightOptions(t)
	o.Lookup = func(string) (string, bool) { return "", false }
	r := inspect(o, "", func(string) (string, error) { return "", errors.New("unavailable") })
	if r.Ready || r.ModelCalled {
		t.Fatal("unprepared launch was enabled")
	}
	for _, c := range r.Checks {
		if (c.ID == "installed_coddy" || strings.HasSuffix(c.ID, ":provider_configuration") ||
			strings.HasSuffix(c.ID, ":credentials_available") || c.ID == "external_execution") && c.Ready {
			t.Fatal("missing prerequisite accepted", c.ID)
		}
	}
}

func TestPreflightBDD_VersionProbeHasNoInheritedCredentials(t *testing.T) {
	t.Setenv("RESEARCH_PRIVATE_SECRET", "private-test-secret")
	dir := t.TempDir()
	binary := filepath.Join(dir, "coddy")
	body := "#!/bin/sh\n[ \"$1\" = --version ] || exit 1\n[ -z \"$RESEARCH_PRIVATE_SECRET\" ] || exit 1\n[ \"$HOME\" = \"$CODDY_HOME\" ] || exit 1\nprintf '1.1.64\\n'\n"
	if err := os.WriteFile(binary, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	version, err := probeVersion(binary)
	if err != nil || version != "1.1.64" {
		t.Fatalf("probe: %s %v", version, err)
	}
	var out, errOut bytes.Buffer
	if run([]string{"-config", filepath.Join(dir, "missing.json")}, &out, &errOut) != 2 {
		t.Fatal("missing config accepted")
	}
	if strings.Contains(out.String()+errOut.String(), "private-test-secret") {
		t.Fatal("secret disclosed")
	}
}
