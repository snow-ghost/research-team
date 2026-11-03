package leancheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type HTTPChecker struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPChecker(baseURL, token string) *HTTPChecker {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &HTTPChecker{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Transport: transport, Timeout: 75 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *HTTPChecker) Check(ctx context.Context, goal Goal, source, dir string) (Report, error) {
	var report Report
	if err := c.post(ctx, "/v1/check", map[string]any{"goal": goal, "source": source, "dir": dir}, &report); err != nil {
		return report, err
	}
	if report.Status == "" || report.GoalSHA256 != Digest(goal) || report.SourceSHA256 != Digest(source) || report.AuditSHA256 != AuditDigest() {
		return report, errors.New("lean service report binding mismatch")
	}
	return report, nil
}

func (c *HTTPChecker) MatchModule(ctx context.Context, goal Goal, moduleDir string, module ModuleReport, candidate, _ string) (MatchReport, error) {
	var report MatchReport
	if err := c.post(ctx, "/v1/match", map[string]any{"goal": goal, "module_dir": moduleDir, "module": module, "candidate": candidate}, &report); err != nil {
		return report, err
	}
	if report.Status == "" || report.GoalSHA256 != Digest(goal) || report.ArtifactSHA256 != module.ArtifactSHA256 || report.MatcherSHA256 != MatcherDigest() {
		return report, errors.New("lean service match binding mismatch")
	}
	return report, nil
}

func (c *HTTPChecker) InspectModule(ctx context.Context, goal Goal, moduleDir string, module ModuleReport, dir string) (SignatureReport, error) {
	var report SignatureReport
	if err := os.MkdirAll(dir, 0700); err != nil {
		return report, err
	}
	if err := c.post(ctx, "/v1/inspect", map[string]any{"goal": goal, "module_dir": moduleDir, "module": module, "dir": dir}, &report); err != nil {
		return report, err
	}
	if report.GoalSHA256 != Digest(goal) || report.ArtifactSHA256 != module.ArtifactSHA256 || report.InspectorSHA256 != InspectorDigest() {
		return report, errors.New("lean service inspection binding mismatch")
	}
	return report, nil
}

func (c *HTTPChecker) PrepareGoals(ctx context.Context, goals []Goal, dir string) (GoalPreparation, error) {
	var report GoalPreparation
	if err := c.post(ctx, "/v1/prepare", map[string]any{"goals": goals, "dir": dir}, &report); err != nil {
		return report, err
	}
	if err := VerifyPreparedDefinitions(dir, report, goals); err != nil {
		return report, err
	}
	return report, nil
}

func (c *HTTPChecker) BuildModule(ctx context.Context, goal Goal, source, module, dir string) (ModuleReport, error) {
	return c.build(ctx, goal, source, module, dir, nil)
}

func (c *HTTPChecker) BuildSharedModule(ctx context.Context, goal Goal, source, module, dir string, definition PreparedDefinition) (ModuleReport, error) {
	return c.build(ctx, goal, source, module, dir, &definition)
}

func (c *HTTPChecker) build(ctx context.Context, goal Goal, source, module, dir string, definition *PreparedDefinition) (ModuleReport, error) {
	var report ModuleReport
	input := map[string]any{"goal": goal, "source": source, "module": module, "dir": dir}
	if definition != nil {
		input["definition"] = *definition
	}
	if err := c.post(ctx, "/v1/build", input, &report); err != nil {
		return report, err
	}
	if err := VerifyModuleArtifacts(filepath.Join(dir, "checked"), report); err != nil {
		return report, err
	}
	return report, nil
}

func (c *HTTPChecker) post(ctx context.Context, path string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("lean service unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("lean service response unavailable")
	}
	if err := json.Unmarshal(data, output); err != nil {
		return errors.New("invalid lean service response")
	}
	return nil
}
