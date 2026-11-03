package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
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
	h.artifactAlias = "/src"
	if got, err := h.artifactDir("/src/preparation"); err != nil || got != inside {
		t.Fatalf("container alias was not mapped to the shared root: %q %v", got, err)
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

func TestEnvironmentOnlyAddsLibrariesInsideSharedRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	inside := filepath.Join(root, "library")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(outside, "base")
	h := &leanHandler{checker: leancheck.DockerChecker{Config: leancheck.Config{Libraries: []string{base}}}, artifactRoot: root, artifactAlias: "/src"}
	environment, err := h.environment([]string{base, "/src/library"})
	if err != nil || !reflect.DeepEqual(environment.Libraries, []string{base, inside}) {
		t.Fatalf("pinned library environment mismatch: %v %v", environment.Libraries, err)
	}
	if _, err := h.environment([]string{inside}); err == nil {
		t.Fatal("replacement of the pinned library prefix accepted")
	}
	if _, err := h.environment([]string{base, outside}); err == nil {
		t.Fatal("additional library outside the shared root accepted")
	}
	if _, err := h.environment(nil); err != nil {
		t.Fatal("default pinned environment rejected", err)
	}
}
