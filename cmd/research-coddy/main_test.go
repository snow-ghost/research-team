package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDoesNotNeedTokenOrStateDirectory(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-config", "../../examples/coddy/connector.json"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Executed bool
		Valid    bool `json:"configuration_valid"`
	}
	if json.Unmarshal(out.Bytes(), &response) != nil || response.Executed || !response.Valid {
		t.Fatal(out.String())
	}
}

func TestPrepareDoesNotNeedGitHubCredentials(t *testing.T) {
	var out bytes.Buffer
	dir := filepath.Join(t.TempDir(), "state")
	err := run([]string{"-config", "../../examples/coddy/connector.json", "-action", "prepare",
		"-job", "../../examples/coddy/job.json", "-state-dir", dir}, &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID          string
		IssueNumber int `json:"issue_number"`
	}
	if json.Unmarshal(out.Bytes(), &response) != nil || len(response.ID) != 64 || response.IssueNumber != 0 {
		t.Fatal(out.String())
	}
	if _, err = os.Stat(filepath.Join(dir, response.ID+".json")); err != nil {
		t.Fatal(err)
	}
}

func TestInlineSecretIsRejectedWithoutEcho(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"token":"private-value"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := run([]string{"-config", path}, &out, &out)
	if err == nil || bytes.Contains([]byte(err.Error()), []byte("private-value")) {
		t.Fatal("inline secret accepted or echoed")
	}
}
