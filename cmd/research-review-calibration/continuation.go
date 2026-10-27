package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/leancheck"
	"github.com/snow-ghost/research-team/internal/proofreview"
)

type continuation struct {
	Directory string
	Rows      []map[string]any
	Tasks     map[string]execution.Task
}

func scheduleIDs(cases []proofreview.CalibrationCase) []string {
	ids := []string{}
	for _, c := range cases {
		for rep := 1; rep <= 2; rep++ {
			arms := []string{"legacy", "typed"}
			if rep == 2 {
				arms[0], arms[1] = arms[1], arms[0]
			}
			for _, arm := range arms {
				ids = append(ids, fmt.Sprintf("%s-%d-%s", c.ID, rep, arm))
			}
		}
	}
	return ids
}

func loadContinuation(directory, suite, profileSHA string, cases []proofreview.CalibrationCase) (*continuation, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(root); err != nil || real != root {
		return nil, errors.New("continuation directory must not be a symlink")
	}
	var authorization struct {
		Suite   string `json:"suite"`
		Profile string `json:"profile_sha256"`
		Maximum int    `json:"max_requests"`
	}
	// These operational files have additional fields, so read them without discarding the original files.
	if err = readOperational(filepath.Join(root, "authorization.json"), &authorization); err != nil {
		return nil, err
	}
	ids := scheduleIDs(cases)
	if authorization.Suite != suite || authorization.Profile != profileSHA || authorization.Maximum != len(ids) {
		return nil, errors.New("continuation cannot change the suite, profile or cumulative limit")
	}
	var reference []proofreview.CalibrationCase
	if err = readOperational(filepath.Join(root, "reference-private.json"), &reference); err != nil || leancheck.Digest(reference) != leancheck.Digest(cases) {
		return nil, errors.New("reference fixtures changed")
	}
	var rows []map[string]any
	if err = readOperational(filepath.Join(root, "stopped-unknown.json"), &rows); err != nil {
		if err = readOperational(filepath.Join(root, "stopped-execution.json"), &rows); err != nil {
			return nil, errors.New("only a terminal stopped run can be continued")
		}
	}
	if len(rows) == 0 || len(rows) >= len(ids) {
		return nil, errors.New("no remaining authorized schedule")
	}
	var inherited struct {
		Rows  []map[string]any          `json:"rows"`
		Tasks map[string]execution.Task `json:"tasks"`
	}
	if _, err = os.Stat(filepath.Join(root, "continuation.json")); err == nil {
		if err = readOperational(filepath.Join(root, "continuation.json"), &inherited); err != nil {
			return nil, err
		}
	}
	if len(inherited.Rows) > len(rows) {
		return nil, errors.New("invalid inherited reservation count")
	}
	out := &continuation{Directory: root, Rows: rows, Tasks: map[string]execution.Task{}}
	for index, row := range rows {
		id := ids[index]
		if row["id"] != id {
			return nil, errors.New("completed records are not the reserved schedule prefix")
		}
		if index < len(inherited.Rows) {
			if leancheck.Digest(row) != leancheck.Digest(inherited.Rows[index]) {
				return nil, errors.New("inherited record changed")
			}
			task, ok := inherited.Tasks[id]
			if !ok {
				return nil, errors.New("inherited task missing")
			}
			out.Tasks[id] = task
			continue
		}
		var reserved struct {
			Request int `json:"request"`
		}
		if err = readOperational(filepath.Join(root, id, "reserved.json"), &reserved); err != nil || reserved.Request != index+1 {
			return nil, errors.New("reservation order changed")
		}
		var summary map[string]any
		if err = readOperational(filepath.Join(root, id, "summary.json"), &summary); err != nil || leancheck.Digest(summary) != leancheck.Digest(row) {
			return nil, errors.New("terminal summary changed")
		}
		var result execution.Result
		if err = readOperational(filepath.Join(root, id, "result.json"), &result); err != nil || row["status"] != result.Status || row["remote_outcome"] != result.RemoteOutcome {
			return nil, errors.New("terminal result changed")
		}
		var packet struct {
			Task execution.Task `json:"task"`
		}
		if err = readOperational(filepath.Join(root, id, "request-packet.json"), &packet); err != nil || packet.Task.ID != fmt.Sprintf("calibration-%03d", index+1) {
			return nil, errors.New("frozen task missing or changed")
		}
		out.Tasks[id] = packet.Task
	}
	for _, id := range ids[len(rows):] {
		if _, err := os.Stat(filepath.Join(root, id, "reserved.json")); !os.IsNotExist(err) {
			return nil, errors.New("unaccounted reservation outside terminal prefix")
		}
	}
	return out, nil
}

func readOperational(path string, target any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return errors.New("invalid operational JSON file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil {
		return err
	}
	if len(body) > 16<<20 {
		return errors.New("operational JSON limit")
	}
	return json.Unmarshal(body, target)
}

func claimContinuation(prior *continuation, destination string) error {
	if prior == nil {
		return nil
	}
	return save(filepath.Join(prior.Directory, "continuation-claim.json"), map[string]any{"destination": destination, "reserved_prefix": len(prior.Rows), "prior_rows_sha256": leancheck.Digest(prior.Rows), "automatic_retry": false})
}
