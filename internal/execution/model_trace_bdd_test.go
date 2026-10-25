package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelTraceBDD_ConnectFailureDoesNotClaimAnUnknownRemoteRequest(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + l.Addr().String() + "/v1"
	_ = l.Close()
	r, err := buildTest(t, modelProfile(url)).Run(context.Background(), taskFor(t))
	if err == nil || r.RemoteOutcome != "not_started" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Kind != "connect_failure" || r.Diagnostics[0].Transmission != "not_sent" || r.Usage != nil {
		t.Fatal(r, err)
	}
	body, _ := json.Marshal(r)
	if strings.Contains(string(body), url) || strings.Contains(string(body), "private-test-token") {
		t.Fatal("trace leaked address or token")
	}
}

func TestModelTraceBDD_DisconnectAfterSendingDoesNotAuthorizeReplay(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fscan(r.Body, new(string))
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer s.Close()
	r, err := buildTest(t, modelProfile(s.URL+"/v1")).Run(context.Background(), taskFor(t))
	if err == nil || r.RemoteOutcome != "unknown" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Transmission != "unknown" || r.Usage != nil {
		t.Fatal(r, err)
	}
}

func TestModelTraceBDD_LengthResponseKeepsFinishReasonAndMeasuredUsage(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}],"usage":{"prompt_tokens":7,"completion_tokens":11}}`)
	}))
	defer s.Close()
	r, err := buildTest(t, modelProfile(s.URL+"/v1")).Run(context.Background(), taskFor(t))
	if err == nil || r.RemoteOutcome != "response_received" || r.Usage == nil || r.Usage.OutputTokens != 11 || len(r.Diagnostics) != 1 || r.Diagnostics[0].FinishReason != "length" || !r.Diagnostics[0].UsageKnown {
		t.Fatal(r, err)
	}
}

func TestModelTraceBDD_UnrecognizedFinishReasonDoesNotCopyProviderText(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"private-test-token"}],"usage":{"prompt_tokens":7,"completion_tokens":11}}`)
	}))
	defer s.Close()
	r, err := buildTest(t, modelProfile(s.URL+"/v1")).Run(context.Background(), taskFor(t))
	if err == nil || r.RemoteOutcome != "response_received" {
		t.Fatal(r, err)
	}
	body, _ := json.Marshal(r)
	if strings.Contains(string(body), "private-test-token") {
		t.Fatal("provider reason leaked token")
	}
}
