package researchcompare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

var ErrIncomplete = errors.New("comparison is incomplete; model work is never resumed here")

type Delivery struct {
	Version       int             `json:"version"`
	Server        string          `json:"server"`
	ReportSHA256  string          `json:"report_sha256"`
	Request       json.RawMessage `json:"request,omitempty"`
	RequestSHA256 string          `json:"request_sha256,omitempty"`
	State         string          `json:"state"`
	Comparison    string          `json:"comparison,omitempty"`
}

type Finalizer struct {
	Server, Token, Pin, Output, Artifacts string
	Client                                *http.Client
}

type comparisonDeliveryRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Report           Report `json:"report"`
	Confirm          bool   `json:"confirm"`
}

// Deliver performs only local analysis and idempotent report delivery. It cannot
// create model attempts, resume the experiment, or accept mathematical results.
func (f Finalizer) Deliver(ctx context.Context, r Report) (Delivery, error) {
	d := Delivery{Version: 1, Server: f.Server, ReportSHA256: leancheck.Digest(r), State: "prepared"}
	u, err := url.Parse(f.Server)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Host == "" || u.Scheme != "https" && !(u.Scheme == "http" && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()) || len(f.Token) < 32 || r.ConfigurationSHA256 != f.Pin || r.Simulation {
		return d, errors.New("invalid destination or unapproved comparison")
	}
	a, err := Analyze(r, f.Artifacts)
	if err != nil {
		return d, err
	}
	if !a.Summary.Complete {
		return d, ErrIncomplete
	}
	if err := os.Mkdir(f.Output, 0700); err != nil && !os.IsExist(err) {
		return d, err
	}
	info, err := os.Lstat(f.Output)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return d, errors.New("finalization directory must be private")
	}
	unlock, err := lockFinalization(f.Output)
	if err != nil {
		return d, err
	}
	defer unlock()
	path := filepath.Join(f.Output, "delivery.json")
	if body, err := os.ReadFile(path); err == nil {
		if len(body) > 3<<20 || json.Unmarshal(body, &d) != nil || d.Version != 1 || d.Server != f.Server || d.ReportSHA256 != leancheck.Digest(r) || len(d.Request) > 0 && d.RequestSHA256 != leancheck.Digest(d.Request) {
			return d, errors.New("saved delivery binding differs")
		}
		if len(d.Request) > 0 {
			var request comparisonDeliveryRequest
			if json.Unmarshal(d.Request, &request) != nil || !request.Confirm || request.ExpectedRevision < 1 || request.RequestID != "finalize-"+d.ReportSHA256[:48] || leancheck.Digest(request.Report) != d.ReportSHA256 {
				return d, errors.New("saved request does not match finalized report")
			}
		}
	} else if !os.IsNotExist(err) {
		return d, err
	}
	for name, value := range map[string]any{"report.json": r, "summary.json": a.Summary, "analysis.json": a} {
		if err := saveFinalJSON(filepath.Join(f.Output, name), value); err != nil {
			return d, err
		}
	}
	if d.State == "acknowledged" {
		return d, nil
	}
	if f.Client == nil {
		f.Client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	revision, existing, err := f.lookup(ctx, d.ReportSHA256)
	if err != nil {
		return d, err
	}
	if existing != "" {
		d.State, d.Comparison = "acknowledged", existing
		return d, saveFinalJSON(path, d)
	}
	if len(d.Request) == 0 {
		d.Request, _ = json.Marshal(comparisonDeliveryRequest{revision, "finalize-" + d.ReportSHA256[:48], r, true})
		if len(d.Request) > 2<<20 {
			return d, errors.New("delivery exceeds server report limit; local bundle retained")
		}
		d.RequestSHA256 = leancheck.Digest(d.Request)
	}
	d.State = "pending"
	if err := saveFinalJSON(path, d); err != nil {
		return d, err
	}
	status, _, err := f.request(ctx, "POST", "/api/comparisons", d.Request)
	if err != nil {
		d.State = "outcome_unknown"
		if saveErr := saveFinalJSON(path, d); saveErr != nil {
			return d, saveErr
		}
		return d, errors.New("delivery outcome unknown; saved request retained")
	}
	if status == 409 {
		// A known revision conflict did not commit this request. A later invocation
		// reconciles the report hash before preparing the next revision.
		d.Request = nil
		d.RequestSHA256 = ""
		d.State = "revision_conflict"
		return d, saveFinalJSON(path, d)
	}
	if status != 200 {
		return d, fmt.Errorf("comparison delivery refused: HTTP %d", status)
	}
	_, existing, err = f.lookup(ctx, d.ReportSHA256)
	if err != nil {
		return d, err
	}
	if existing == "" {
		return d, errors.New("delivery not observed in durable state")
	}
	d.State, d.Comparison = "acknowledged", existing
	return d, saveFinalJSON(path, d)
}

func (f Finalizer) lookup(ctx context.Context, digest string) (int, string, error) {
	status, body, err := f.request(ctx, "GET", "/api/bootstrap", nil)
	if err != nil || status != 200 {
		return 0, "", errors.New("cannot reconcile comparison state")
	}
	var b struct {
		State struct {
			Revision    int `json:"revision"`
			Comparisons []struct {
				ID  string `json:"id"`
				SHA string `json:"report_sha256"`
			} `json:"comparisons"`
		} `json:"state"`
	}
	if json.Unmarshal(body, &b) != nil || b.State.Revision < 1 {
		return 0, "", errors.New("invalid comparison state")
	}
	for _, record := range b.State.Comparisons {
		if record.SHA == digest {
			return b.State.Revision, record.ID, nil
		}
	}
	return b.State.Revision, "", nil
}

func (f Finalizer) request(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	r, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(f.Server, "/")+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	r.Header.Set("Authorization", "Bearer "+f.Token)
	r.Header.Set("Content-Type", "application/json")
	resp, err := f.Client.Do(r)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
	if err != nil || len(b) > 32<<20 {
		return 0, nil, errors.New("comparison response exceeds bound")
	}
	return resp.StatusCode, b, nil
}

func saveFinalJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".finalize-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
