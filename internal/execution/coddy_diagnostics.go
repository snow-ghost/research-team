package execution

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

const diagnosticTextLimit = 4096

// Diagnostics are operator artifacts, never proof content or model context.
type Diagnostic struct {
	Source              string          `json:"source"`
	Stage               string          `json:"stage"`
	Kind                string          `json:"kind"`
	Message             string          `json:"message,omitempty"`
	RPCCode             *int            `json:"rpc_code,omitempty"`
	Data                json.RawMessage `json:"data,omitempty"`
	DataOmitted         bool            `json:"data_omitted,omitempty"`
	ElapsedMilliseconds int64           `json:"elapsed_ms,omitempty"`
	ExitCode            *int            `json:"exit_code,omitempty"`
	Signal              string          `json:"signal,omitempty"`
	ClientStopRequested bool            `json:"client_stop_requested,omitempty"`
	PayloadBytes        int64           `json:"payload_bytes,omitempty"`
	Truncated           bool            `json:"truncated,omitempty"`
	ByteLimitReached    bool            `json:"byte_limit_reached,omitempty"`
}

var diagnosticMasks = []struct {
	pattern *regexp.Regexp
	replace string
}{
	{regexp.MustCompile(`(?i)("(?:authorization|x-api-key|api[_-]?key|access[_-]?token|token|password|secret)"\s*:\s*)"(?:\\.|[^"\\])*"`), `${1}"[REDACTED]"`},
	{regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[^\s"'<>]+`), `${1} [REDACTED]`},
	{regexp.MustCompile(`(?i)([?&](?:api[_-]?key|access[_-]?token|token|password|secret|key)=)[^&\s"'<>]*`), `${1}[REDACTED]`},
	{regexp.MustCompile(`(?i)(https?://)[^/\s@]+@`), `${1}[REDACTED]@`},
	{regexp.MustCompile(`(?im)(\b(?:Authorization|Proxy-Authorization|X-API-Key)\s*:\s*)[^\r\n]+`), `${1}[REDACTED]`},
	{regexp.MustCompile(`(?i)(\b(?:OPENAI_API_KEY|ANTHROPIC_API_KEY|api[_-]?key|access[_-]?token|password|secret)\s*=\s*)[^\s"',;&<>]+`), `${1}[REDACTED]`},
}

func redactDiagnostic(value, token string) string {
	if token != "" {
		encoded, _ := json.Marshal(token)
		for _, secret := range []string{token, string(encoded[1 : len(encoded)-1]), url.QueryEscape(token), url.PathEscape(token)} {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	for _, mask := range diagnosticMasks {
		value = mask.pattern.ReplaceAllString(value, mask.replace)
	}
	return strings.ToValidUTF8(value, "\uFFFD")
}

func addDiagnostic(ctx context.Context, result *Result, d Diagnostic) {
	if len(result.Diagnostics) >= 16 {
		return
	}
	result.Diagnostics = append(result.Diagnostics, d)
	body, _ := json.Marshal(d)
	record(ctx, result, Event{Type: "executor_diagnostic", Status: d.Stage, Output: string(body)})
}

func recordACPFailure(ctx context.Context, result *Result, stage string, started time.Time, err error, token string) {
	if err == nil {
		return
	}
	d := Diagnostic{Source: "acp", Stage: stage, Kind: "transport_error", ElapsedMilliseconds: time.Since(started).Milliseconds()}
	var rpc *acp.RequestError
	if errors.As(err, &rpc) {
		d.Kind, d.RPCCode = "rpc_error", &rpc.Code
		d.Message = redactDiagnostic(rpc.Message, token)
		if rpc.Data != nil {
			d.Data, d.DataOmitted = diagnosticData(rpc.Data, token)
		}
	} else {
		d.Message = redactDiagnostic(err.Error(), token)
	}
	d.Truncated = len(d.Message) > diagnosticTextLimit
	d.Message = Preview(d.Message, diagnosticTextLimit)
	addDiagnostic(ctx, result, d)
}

func diagnosticData(value any, token string) (json.RawMessage, bool) {
	// Keep diagnostic fields, not arbitrary provider bodies, prompts or headers.
	body, err := json.Marshal(value)
	if err != nil || len(body) > 65536 {
		return nil, true
	}
	var decoded any
	if json.Unmarshal(body, &decoded) != nil {
		return nil, true
	}
	omitted := false
	var selectData func(any, int) any
	selectData = func(v any, depth int) any {
		if depth > 4 {
			omitted = true
			return nil
		}
		switch v := v.(type) {
		case map[string]any:
			out := map[string]any{}
			for k, item := range v {
				switch strings.ToLower(k) {
				case "message", "error", "code", "type", "status", "status_code", "http_status", "request_id", "retry_after", "cause", "details":
					out[k] = selectData(item, depth+1)
				default:
					omitted = true
				}
			}
			return out
		case string:
			v = redactDiagnostic(v, token)
			if len(v) > 1024 {
				omitted = true
			}
			return Preview(v, 1024)
		case nil, bool, float64:
			return v
		default:
			omitted = true
			return nil
		}
	}
	selected := selectData(decoded, 0)
	body, err = json.Marshal(selected)
	if err != nil || len(body) > 2048 {
		return nil, true
	}
	return body, omitted
}

type diagnosticTail struct {
	mu    sync.Mutex
	body  []byte
	bytes int64
}

func (t *diagnosticTail) write(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bytes += int64(len(p))
	const capacity = 65536
	if len(p) >= capacity {
		t.body = append(t.body[:0], p[len(p)-capacity:]...)
		return
	}
	if excess := len(t.body) + len(p) - capacity; excess > 0 {
		copy(t.body, t.body[excess:])
		t.body = t.body[:len(t.body)-excess]
	}
	t.body = append(t.body, p...)
}

func (t *diagnosticTail) snapshot() (string, int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	value := string(t.body)
	truncated := t.bytes > int64(len(t.body))
	if truncated {
		if i := strings.IndexByte(value, '\n'); i >= 0 {
			value = value[i+1:]
		} else {
			value = ""
		}
	}
	// Drop unfinished boundary lines: a killed writer may stop inside a key.
	if value != "" && !strings.HasSuffix(value, "\n") {
		truncated = true
		if i := strings.LastIndexByte(value, '\n'); i >= 0 {
			value = value[:i+1]
		} else {
			value = ""
		}
	}
	return value, t.bytes, truncated
}

type acpProcessReport struct {
	stderr              string
	stderrBytes         int64
	stderrTruncated     bool
	exitCode            *int
	signal              string
	clientStopRequested bool
	elapsedMS           int64
	byteLimitReached    bool
}

func recordProcessDiagnostics(ctx context.Context, result *Result, report acpProcessReport, token string) {
	addDiagnostic(ctx, result, Diagnostic{Source: "process", Stage: "process", Kind: "process_exit", ExitCode: report.exitCode,
		Signal: report.signal, ClientStopRequested: report.clientStopRequested, ElapsedMilliseconds: report.elapsedMS, ByteLimitReached: report.byteLimitReached})
	if report.stderrBytes == 0 {
		return
	}
	value := redactDiagnostic(report.stderr, token)
	truncated := report.stderrTruncated || len(value) > diagnosticTextLimit
	if len(value) > diagnosticTextLimit {
		value = value[len(value)-diagnosticTextLimit:]
		if i := strings.IndexByte(value, '\n'); i >= 0 {
			value = value[i+1:]
		} else {
			value = ""
		}
	}
	addDiagnostic(ctx, result, Diagnostic{Source: "stderr", Stage: "process", Kind: "stderr_tail", Message: value, PayloadBytes: report.stderrBytes, Truncated: truncated})
}
