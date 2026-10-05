//go:build linux

package execution

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	acp "github.com/coder/acp-go-sdk"
)

func TestCoddyAgentBDD_RPCAndProcessDiagnosticsAreRetainedWithoutSecrets(t *testing.T) {
	for _, scenario := range []string{"prompt_error", "setting_error", "process_crash", "stderr_tail"} {
		t.Run(scenario, func(t *testing.T) {
			r, _ := buildTest(t, coddyProfile(coddyFixture(t, scenario))).Run(context.Background(), taskFor(t))
			body, _ := json.Marshal(r)
			if strings.Contains(string(body), "private-test-token") || strings.Contains(string(body), "OTHER_CREDENTIAL") || strings.Contains(string(body), "PRIVATE_ERROR_BODY") {
				t.Fatal("diagnostics leaked a credential or unselected error data")
			}
			var view struct {
				Diagnostics []struct {
					Source      string          `json:"source"`
					Stage       string          `json:"stage"`
					Message     string          `json:"message"`
					RPCCode     *int            `json:"rpc_code"`
					ExitCode    *int            `json:"exit_code"`
					Data        json.RawMessage `json:"data"`
					DataOmitted bool            `json:"data_omitted"`
					Truncated   bool            `json:"truncated"`
				} `json:"diagnostics"`
			}
			if err := json.Unmarshal(body, &view); err != nil || len(view.Diagnostics) == 0 {
				t.Fatal("diagnostics missing", err)
			}
			foundRPC, foundProcess, foundStderr := false, false, false
			for _, d := range view.Diagnostics {
				if d.Source == "acp" {
					foundRPC = true
					if scenario == "prompt_error" && (d.Stage != "prompt" || d.RPCCode == nil || *d.RPCCode != -32603 || !strings.Contains(d.Message, "model did not respond") || !strings.Contains(string(d.Data), "request-503") || !d.DataOmitted) {
						t.Fatal("original RPC failure lost", d)
					}
					if scenario == "setting_error" && (d.Stage != "configure:mode" || d.RPCCode == nil || *d.RPCCode != -32602) {
						t.Fatal("configuration failure lost", d)
					}
				}
				if d.Source == "process" {
					foundProcess = true
					if scenario == "process_crash" && (d.ExitCode == nil || *d.ExitCode != 42) {
						t.Fatal("exit code lost", d)
					}
				}
				if d.Source == "stderr" {
					foundStderr = true
					if scenario == "stderr_tail" && (!d.Truncated || !strings.Contains(d.Message, "last diagnostic")) {
						t.Fatal("bounded stderr tail missing", d)
					}
					if len(d.Message) > 4200 {
						t.Fatal("stderr preview unbounded")
					}
				}
			}
			if !foundProcess || (scenario != "stderr_tail" && !foundRPC) || (scenario != "setting_error" && !foundStderr) {
				t.Fatalf("missing diagnostic source: rpc=%v process=%v stderr=%v", foundRPC, foundProcess, foundStderr)
			}
		})
	}
}

func TestCoddyAgentBDD_RealBinaryProviderFailureHasNativeDiagnostics(t *testing.T) {
	binary := os.Getenv("CODDY_AGENT_TEST_BINARY")
	if binary == "" {
		t.Skip("set CODDY_AGENT_TEST_BINARY; only a local failing provider is used")
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"test/model","object":"model"}]}`)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"fixture provider unavailable private-test-token","type":"server_error","code":"fixture_503"}}`)
	}))
	defer server.Close()
	p := coddyProfile(binary)
	if version := os.Getenv("CODDY_AGENT_TEST_VERSION"); version != "" {
		p.External.ExpectedVersion = version
	}
	p.External.BaseURL, p.External.AllowLoopbackHTTP = server.URL+"/v1", true
	r, err := buildTest(t, p).Run(context.Background(), taskFor(t))
	if err == nil || r.Candidate != "" || calls.Load() < 1 {
		t.Fatal("provider failure was not rejected", err)
	}
	body, _ := json.Marshal(r)
	if strings.Contains(string(body), "private-test-token") {
		t.Fatal("native provider diagnostic leaked credential")
	}
	found := false
	for _, d := range r.Diagnostics {
		if d.Source == "acp" && d.Stage == "prompt" && d.RPCCode != nil && *d.RPCCode == -32603 && strings.Contains(d.Message, "fixture provider unavailable") {
			found = true
		}
	}
	if !found {
		t.Fatalf("native error not preserved: %+v", r.Diagnostics)
	}
}

func TestCoddyDiagnostics_RedactionPrecedesTruncationAndDataIsSelected(t *testing.T) {
	token := `private&"key`
	r := Result{}
	message := "encoded credential " + url.QueryEscape(token) + " Bearer OTHER_CREDENTIAL " + strings.Repeat("x", 9000)
	recordACPFailure(context.Background(), &r, "prompt", time.Now(), &acp.RequestError{Code: -32603, Message: message,
		Data: map[string]any{"message": token, "details": map[string]any{"http_status": 503, "authorization": "OTHER_CREDENTIAL"}, "raw_response": "PRIVATE_ERROR_BODY"}}, token)
	body, err := json.Marshal(r)
	if err != nil || strings.Contains(string(body), "OTHER_CREDENTIAL") || strings.Contains(string(body), "PRIVATE_ERROR_BODY") || strings.Contains(string(body), url.QueryEscape(token)) {
		t.Fatal("credential or raw data leaked", err)
	}
	if len(r.Diagnostics) != 1 || !r.Diagnostics[0].Truncated || !r.Diagnostics[0].DataOmitted || len(r.Diagnostics[0].Message) > 4200 {
		t.Fatal("diagnostic bounds or omissions not recorded")
	}
	r = Result{}
	recordACPFailure(context.Background(), &r, "prompt", time.Now(), errors.New("transport closed"), token)
	if r.Diagnostics[0].Kind != "transport_error" || r.Diagnostics[0].RPCCode != nil || r.Diagnostics[0].Message != "transport closed" {
		t.Fatal("transport failure mislabeled as remote RPC")
	}
}

func TestCoddyDiagnostics_TailDropsIncompleteCredentialAndUTF8Boundaries(t *testing.T) {
	tail := &diagnosticTail{}
	tail.write([]byte(strings.Repeat("old line\n", 9000)))
	tail.write([]byte("complete line\npartial credential private-test-"))
	text, size, truncated := tail.snapshot()
	if !truncated || size < 65536 || strings.Contains(text, "partial credential") || !strings.Contains(text, "complete line") {
		t.Fatal("unsafe or missing stderr boundary")
	}
	r := Result{}
	recordProcessDiagnostics(context.Background(), &r, acpProcessReport{stderr: strings.Repeat("\u03B1\n", 20000), stderrBytes: 60000}, "")
	if len(r.Diagnostics) != 2 || !utf8.ValidString(r.Diagnostics[1].Message) || len(r.Diagnostics[1].Message) > 4200 || !r.Diagnostics[1].Truncated {
		t.Fatal("invalid or unbounded stderr")
	}
}
