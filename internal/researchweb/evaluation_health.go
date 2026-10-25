package researchweb

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/snow-ghost/research-team/internal/researchcompare"
)

type EvaluationHealth struct {
	ProcessState             string     `json:"process_state"`
	ProcessObservationFresh  bool       `json:"process_observation_fresh"`
	ProgressObservationFresh bool       `json:"progress_observation_fresh"`
	ReportStale              bool       `json:"report_stale"`
	NeedsAttention           bool       `json:"needs_attention"`
	Phase                    string     `json:"phase"`
	Case                     string     `json:"case,omitempty"`
	Mode                     string     `json:"mode,omitempty"`
	Role                     string     `json:"role,omitempty"`
	ObservedAt               *time.Time `json:"observed_at,omitempty"`
	LastEventAt              *time.Time `json:"last_event_at,omitempty"`
}

func evaluationHealth(w EvaluationWatch, updated *time.Time, complete bool, now time.Time) *EvaluationHealth {
	h := &EvaluationHealth{ProcessState: "unknown", Phase: "last_checkpoint_only"}
	stale := w.StaleSeconds
	if stale == 0 {
		stale = 1200
	}
	h.ReportStale = !complete && updated != nil && now.Sub(*updated) > time.Duration(stale)*time.Second
	read := func(file string) (researchcompare.Progress, bool) {
		var p researchcompare.Progress
		if file == "" {
			return p, false
		}
		b, err := readBoundedFile(file, 16000)
		if err != nil || json.Unmarshal(b, &p) != nil || p.ConfigurationSHA256 != w.ConfigurationSHA256 || p.ObservedAt.IsZero() || p.ObservedAt.After(now.Add(10*time.Second)) {
			return p, false
		}
		return p, now.Sub(p.ObservedAt) <= 45*time.Second
	}
	if p, fresh := read(w.ProcessFile); fresh && slices.Contains([]string{"running", "stopped", "failed", "completed"}, p.ProcessState) {
		h.ProcessState = p.ProcessState
		h.ProcessObservationFresh = true
		h.ObservedAt = &p.ObservedAt
	}
	if p, fresh := read(w.ProgressFile); fresh && slices.Contains([]string{"preparation", "waiting_model", "model_response", "lean_check", "tool_execution", "tool_completed", "result_processing", "finished"}, p.Phase) && !p.LastEventAt.IsZero() && !p.LastEventAt.After(now.Add(10*time.Second)) {
		h.ProgressObservationFresh = true
		h.Phase = p.Phase
		h.Case = p.Case
		h.Mode = p.Mode
		h.Role = p.Role
		h.LastEventAt = &p.LastEventAt
		if w.ProcessFile == "" && slices.Contains([]string{"running", "stopped", "failed", "completed"}, p.ProcessState) {
			h.ProcessState = p.ProcessState
			h.ProcessObservationFresh = true
			h.ObservedAt = &p.ObservedAt
		}
	}
	h.NeedsAttention = !complete && (h.ProcessState == "stopped" || h.ProcessState == "failed" || h.ProcessState == "completed" || h.ReportStale || w.ProcessFile != "" && !h.ProcessObservationFresh)
	return h
}
