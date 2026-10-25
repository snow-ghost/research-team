package researchcompare

import (
	"context"
	"github.com/snow-ghost/research-team/internal/execution"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProgressBDD_OnlyNamedVisiblePhasesAreRecorded(t *testing.T) {
	if visiblePhase(execution.Event{Type: "model_requested", Input: "PRIVATE", Output: "SECRET"}) != "waiting_model" || visiblePhase(execution.Event{Type: "reasoning", Output: "PRIVATE"}) != "" {
		t.Fatal("unsafe event observation")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "progress.json")
	p, err := NewProgressWriter(file, strings.Repeat("a", 64), false)
	if err != nil {
		t.Fatal(err)
	}
	p.Observe(Progress{Case: "task-001", Role: "review", Phase: "waiting_model"})
	if err := p.Finish("completed"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "SECRET") || !strings.Contains(string(b), "completed") {
		t.Fatal("unsafe progress", string(b))
	}
	if _, err := NewProgressWriter(file, strings.Repeat("a", 64), false); err == nil {
		t.Fatal("observer file silently reused")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.Heartbeat(ctx)
}
