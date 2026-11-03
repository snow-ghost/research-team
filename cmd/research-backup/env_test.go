package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateEnvLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "postgres.env")
	if err := os.WriteFile(path, []byte("RESEARCH_DATABASE_URL='postgres://localhost/research'\nQUOTED=\"value\\n\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	lookup, err := privateEnvLookup(path)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := lookup("RESEARCH_DATABASE_URL"); !ok || value != "postgres://localhost/research" {
		t.Fatal("database URL was not loaded", value, ok)
	}
	if value, ok := lookup("QUOTED"); !ok || value != "value\n" {
		t.Fatal("quoted value was not decoded", value, ok)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := privateEnvLookup(path); err == nil {
		t.Fatal("public environment file accepted")
	}
}
