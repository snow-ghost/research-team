package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactDirStaysInsideSharedRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	h := &leanHandler{artifactRoot: root}
	inside := filepath.Join(root, "preparation")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := h.artifactDir(inside); err != nil || got != inside {
		t.Fatalf("valid artifact directory rejected: %q %v", got, err)
	}
	if _, err := h.artifactDir(root); err == nil {
		t.Fatal("shared root itself accepted as an operation directory")
	}
	if _, err := h.artifactDir(outside); err == nil {
		t.Fatal("outside artifact directory accepted")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := h.artifactDir(link); err == nil {
		t.Fatal("symlink outside the shared root accepted")
	}
}
