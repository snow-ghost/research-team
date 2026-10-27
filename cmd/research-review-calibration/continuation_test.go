package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/proofreview"
)

func stoppedFixture(t *testing.T) (string, []proofreview.CalibrationCase) {
	t.Helper()
	dir := t.TempDir()
	cases := proofreview.CalibrationCases()[:1]
	id := scheduleIDs(cases)[0]
	for file, value := range map[string]any{
		"authorization.json":     map[string]any{"suite": "core", "profile_sha256": "profile", "max_requests": 4},
		"reference-private.json": cases,
	} {
		if err := save(filepath.Join(dir, file), value); err != nil {
			t.Fatal(err)
		}
	}
	row := map[string]any{"id": id, "status": "timed_out", "remote_outcome": "unknown"}
	if err := save(filepath.Join(dir, "stopped-unknown.json"), []map[string]any{row}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, id), 0700); err != nil {
		t.Fatal(err)
	}
	for file, value := range map[string]any{
		"reserved.json": map[string]any{"request": 1}, "summary.json": row,
		"result.json":         execution.Result{Status: "timed_out", RemoteOutcome: "unknown"},
		"request-packet.json": map[string]any{"task": execution.Task{ID: "calibration-001", Objective: "frozen", Context: "frozen"}},
	} {
		if err := save(filepath.Join(dir, id, file), value); err != nil {
			t.Fatal(err)
		}
	}
	return dir, cases
}

func TestContinuationBDD_UnknownRequestStaysChargedAndCannotBeClaimedTwice(t *testing.T) {
	dir, cases := stoppedFixture(t)
	p, err := loadContinuation(dir, "core", "profile", cases)
	if err != nil || len(p.Rows) != 1 || p.Rows[0]["remote_outcome"] != "unknown" || len(p.Tasks) != 1 {
		t.Fatal("unknown prefix lost", err)
	}
	if err = claimContinuation(p, "new-private-run"); err != nil {
		t.Fatal(err)
	}
	if claimContinuation(p, "second-run") == nil {
		t.Fatal("allocation claimed twice")
	}
	if _, err = loadContinuation(dir, "expanded", "profile", cases); err == nil {
		t.Fatal("suite changed")
	}
	if _, err = loadContinuation(dir, "core", "different", cases); err == nil {
		t.Fatal("profile changed")
	}
	changed := append([]proofreview.CalibrationCase{}, cases...)
	changed[0].Statement = "changed"
	if _, err = loadContinuation(dir, "core", "profile", changed); err == nil {
		t.Fatal("fixture changed")
	}
}

func TestContinuationBDD_UnrecordedReservationBlocksContinuation(t *testing.T) {
	dir, cases := stoppedFixture(t)
	id := scheduleIDs(cases)[1]
	if err := os.Mkdir(filepath.Join(dir, id), 0700); err != nil {
		t.Fatal(err)
	}
	if err := save(filepath.Join(dir, id, "reserved.json"), map[string]any{"request": 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadContinuation(dir, "core", "profile", cases); err == nil {
		t.Fatal("unknown extra reservation discarded")
	}
}

func TestContinuationBDD_SecondGenerationKeepsUnknownPrefixAndCumulativeOrder(t *testing.T) {
	parent, cases := stoppedFixture(t)
	previous, err := loadContinuation(parent, "core", "profile", cases)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	id := scheduleIDs(cases)[1]
	row := map[string]any{"id": id, "status": "failed", "remote_outcome": "response_received"}
	rows := append(append([]map[string]any{}, previous.Rows...), row)
	for file, value := range map[string]any{
		"authorization.json":     map[string]any{"suite": "core", "profile_sha256": "profile", "max_requests": 4},
		"reference-private.json": cases,
		"continuation.json":      map[string]any{"rows": previous.Rows, "tasks": previous.Tasks},
		"stopped-execution.json": rows,
	} {
		if err := save(filepath.Join(dir, file), value); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, id), 0700); err != nil {
		t.Fatal(err)
	}
	for file, value := range map[string]any{
		"reserved.json": map[string]any{"request": 2}, "summary.json": row,
		"result.json":         execution.Result{Status: "failed", RemoteOutcome: "response_received"},
		"request-packet.json": map[string]any{"task": execution.Task{ID: "calibration-002", Objective: "frozen-2", Context: "frozen-2"}},
	} {
		if err := save(filepath.Join(dir, id, file), value); err != nil {
			t.Fatal(err)
		}
	}
	next, err := loadContinuation(dir, "core", "profile", cases)
	if err != nil || len(next.Rows) != 2 || len(next.Tasks) != 2 || next.Rows[0]["remote_outcome"] != "unknown" || next.Tasks[id].ID != "calibration-002" {
		t.Fatal("second generation reset an inherited reservation", err)
	}
}
