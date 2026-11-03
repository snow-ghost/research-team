package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/researchweb"
)

func TestReleaseMaintenanceResumesPriorQueue(t *testing.T) {
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token-0123456789abcdef0123456789abcdef" {
			t.Error("missing access token")
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/bootstrap" {
			state := map[string]any{"revision": 7, "paused": step < 2}
			if step == 0 {
				state["maintenance"] = map[string]any{"queue": map[string]string{}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"state": state})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/maintenance" && step == 0 {
			var request researchweb.MaintenanceRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Kind != "release" || !request.Confirm || request.ExpectedRevision != 7 {
				t.Error("invalid maintenance release", request, err)
			}
			step = 1
		} else if r.Method == http.MethodPost && r.URL.Path == "/api/actions" && step == 1 {
			var request researchweb.Action
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Type != "PAUSE" || request.ExpectedRevision != 7 {
				t.Error("invalid queue resume", request, err)
			}
			step = 2
		} else {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"state": map[string]any{"revision": 8, "paused": step < 2}})
	}))
	defer server.Close()
	o := researchweb.Options{Lookup: func(string) (string, bool) { return "test-token-0123456789abcdef0123456789abcdef", true }}
	o.Config.Listen = strings.TrimPrefix(server.URL, "http://")
	o.Config.TokenEnv = "TEST_TOKEN"
	if err := releaseMaintenance(o); err != nil {
		t.Fatal(err)
	}
	if step != 2 {
		t.Fatal("maintenance was not released and queue was not resumed")
	}
}
