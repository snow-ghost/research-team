package researchcompare

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

type Progress struct {
	ConfigurationSHA256 string    `json:"configuration_sha256"`
	ObservedAt          time.Time `json:"observed_at"`
	LastEventAt         time.Time `json:"last_event_at"`
	ProcessState        string    `json:"process_state"`
	Case                string    `json:"case,omitempty"`
	Mode                string    `json:"mode,omitempty"`
	Repetition          int       `json:"repetition,omitempty"`
	Role                string    `json:"role,omitempty"`
	Phase               string    `json:"phase"`
}

// Only named visible actions are observed. Prompts, responses, private reasoning,
// tool arguments, errors and credential values never enter this progress file.
func visiblePhase(e execution.Event) string {
	switch e.Type {
	case "model_requested":
		return "waiting_model"
	case "model_finish":
		return "model_response"
	case "tool_started", "tool_requested":
		if e.Tool == "check_lean" || e.Tool == "check_refutation" {
			return "lean_check"
		}
		return "tool_execution"
	case "tool_finished", "tool_completed":
		return "tool_completed"
	}
	return ""
}

type ProgressWriter struct {
	mu    sync.Mutex
	path  string
	value Progress
	err   error
}

func NewProgressWriter(path, pin string, resume bool) (*ProgressWriter, error) {
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("progress directory must already exist and be private")
	}
	if b, err := os.ReadFile(path); err == nil {
		var previous Progress
		if !resume || json.Unmarshal(b, &previous) != nil || previous.ConfigurationSHA256 != pin {
			return nil, errors.New("existing progress file requires explicit unchanged resume")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	now := time.Now().UTC()
	p := &ProgressWriter{path: path, value: Progress{ConfigurationSHA256: pin, ObservedAt: now, LastEventAt: now, ProcessState: "running", Phase: "preparation"}}
	return p, p.write()
}
func (p *ProgressWriter) write() error {
	p.value.ObservedAt = time.Now().UTC()
	err := saveFinalJSON(p.path, p.value)
	if err != nil {
		p.err = err
	}
	return err
}
func (p *ProgressWriter) Observe(v Progress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v.ConfigurationSHA256 = p.value.ConfigurationSHA256
	v.ProcessState = "running"
	v.LastEventAt = time.Now().UTC()
	p.value = v
	_ = p.write()
}
func (p *ProgressWriter) Heartbeat(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			_ = p.write()
			p.mu.Unlock()
		}
	}
}
func (p *ProgressWriter) Finish(state string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.value.ProcessState = state
	p.value.Phase = "finished"
	p.value.LastEventAt = time.Now().UTC()
	err := p.write()
	if err != nil {
		return err
	}
	return p.err
}
