package leancheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathsBDD_ExplicitResolutionKeepsPinnedVersionAndOriginal(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	alias := filepath.Join(root, "alias")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	c := Config{Runtime: "/usr/bin/docker", Image: "sha256:" + strings.Repeat("a", 64), Toolchain: alias, ExpectedVersion: "pinned", Libraries: []string{alias}, LibraryRevision: "fixed", TimeoutSeconds: 60, MemoryMB: 512}
	if c.Validate() == nil {
		t.Fatal("implicit symlink accepted")
	}
	resolved, err := ResolvePaths(c)
	if err != nil || resolved.Toolchain != actual || resolved.Libraries[0] != actual || c.Libraries[0] != alias || resolved.ExpectedVersion != c.ExpectedVersion || resolved.LibraryRevision != c.LibraryRevision {
		t.Fatal(resolved, err)
	}
	c.Libraries = []string{filepath.Join(root, "missing")}
	if _, err := ResolvePaths(c); err == nil {
		t.Fatal("missing library accepted")
	}
}
