package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelDiagnosticsBDD_HTTPStatusSavedWithoutProviderBodyOrToken(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":"private-test-token","prompt":"PRIVATE-PROVIDER-BODY"}`)
	}))
	defer provider.Close()
	r, err := buildTest(t, modelProfile(provider.URL+"/v1")).Run(context.Background(), taskFor(t))
	if err == nil || r.Status != "failed" || r.RemoteOutcome != "response_received" || r.Usage != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Kind != "http_error" {
		t.Fatal("HTTP failure lost status or invented usage", r, err)
	}
	var metadata struct {
		Status int `json:"http_status"`
	}
	if json.Unmarshal(r.Diagnostics[0].Data, &metadata) != nil || metadata.Status != 429 {
		t.Fatal("missing provider status")
	}
	body, _ := json.Marshal(r)
	if strings.Contains(string(body), "private-test-token") || strings.Contains(string(body), "PRIVATE-PROVIDER-BODY") {
		t.Fatal("diagnostic copied provider body")
	}
}
