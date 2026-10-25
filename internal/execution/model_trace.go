package execution

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http/httptrace"
	"sync"
)

func safeFinishReason(value string) string {
	switch value {
	case "stop", "length", "tool_calls", "content_filter":
		return value
	default:
		return "unrecognized"
	}
}

type completionError struct {
	category string
	stage    string
	status   int
	cause    error
}

func (e *completionError) Error() string {
	if e.status != 0 {
		return "model HTTP failure"
	}
	return "model " + e.category
}
func (e *completionError) Unwrap() error { return e.cause }

// No URLs, headers, certificate names or provider bodies enter this trace.
type modelTrace struct {
	mu                                                         sync.Mutex
	connected, headersWritten, requestWritten, responseHeaders bool
	stage, category                                            string
	status                                                     int
}

func (t *modelTrace) context(ctx context.Context) context.Context {
	set := func(stage string, f func()) { t.mu.Lock(); defer t.mu.Unlock(); t.stage = stage; f() }
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { set("dns", func() {}) },
		ConnectStart:         func(_, _ string) { set("connect", func() {}) },
		TLSHandshakeStart:    func() { set("tls", func() {}) },
		TLSHandshakeDone:     func(tls.ConnectionState, error) {},
		GotConn:              func(httptrace.GotConnInfo) { set("connected", func() { t.connected = true }) },
		WroteHeaders:         func() { set("headers_written", func() { t.headersWritten = true }) },
		WroteRequest:         func(i httptrace.WroteRequestInfo) { set("request_write", func() { t.requestWritten = i.Err == nil }) },
		GotFirstResponseByte: func() { set("response_started", func() {}) },
	})
}

func (t *modelTrace) transportError(err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	category := "transport_failure"
	var dns *net.DNSError
	var op *net.OpError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		category = "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		category = "cancelled"
	case errors.As(err, &dns):
		category = "dns_failure"
	case errors.As(err, &op) && op.Op == "dial":
		category = "connect_failure"
	case t.stage == "tls":
		category = "tls_failure"
	}
	t.category = category
	return &completionError{category: category, stage: t.stage, cause: err}
}

func (t *modelTrace) observation() (stage, transmission, category string, status int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	transmission = "unknown"
	if t.responseHeaders {
		transmission = "response_received"
	} else if !t.connected && !t.headersWritten && (t.category == "dns_failure" || t.category == "connect_failure" || t.category == "tls_failure" || t.stage == "preparation") {
		transmission = "not_sent"
	}
	return t.stage, transmission, t.category, t.status
}
