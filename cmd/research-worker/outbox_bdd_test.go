package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/snow-ghost/research-team/internal/execution"
	"github.com/snow-ghost/research-team/internal/researchweb"
)

func TestDeliveryBDD_LostAcknowledgementResendsSameResultWithoutExecution(t *testing.T) {
	var received []researchweb.WorkerRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/workers/worker-a/attempts/run-a/result" || r.Header.Get("Authorization") != "Bearer test-worker-key" {
			t.Error("wrong delivery destination")
		}
		var request researchweb.WorkerRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		received = append(received, request)
		if len(received) == 1 {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			connection.Close()
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	conf := config{Server: server.URL, Worker: "worker-a", Profile: "missing-profile.json"}
	c := client{config: conf, key: "test-worker-key", http: server.Client()}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	request := researchweb.WorkerRequest{RequestID: "stable-request", Epoch: 4, Snapshot: "snapshot", Result: &execution.Result{AttemptID: "run-a", LeaseEpoch: 4, Snapshot: "snapshot", Status: "candidate", Candidate: "known result"}}
	if err := saveDelivery(dir, deliveryRecord{Version: 1, Server: conf.Server, Worker: conf.Worker, Attempt: "run-a", Request: request}); err != nil {
		t.Fatal(err)
	}
	if err := deliverSaved(context.Background(), c, dir); err == nil {
		t.Fatal("lost acknowledgement treated as success")
	}
	if r, err := loadDelivery(dir, conf); err != nil || r.Acknowledged {
		t.Fatal(r, err)
	}
	if err := deliverSaved(context.Background(), c, dir); err != nil {
		t.Fatal(err)
	}
	if err := deliverSaved(context.Background(), c, dir); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 || deliveryDigest(received[0]) != deliveryDigest(received[1]) {
		t.Fatal("delivery changed or acknowledged result was resent")
	}
	if r, err := loadDelivery(dir, conf); err != nil || !r.Acknowledged {
		t.Fatal(r, err)
	}
}

func TestDeliveryBDD_RejectsTamperingDestinationAndPublicDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	conf := config{Server: "https://example.invalid", Worker: "worker-a"}
	r := deliveryRecord{Version: 1, Server: conf.Server, Worker: conf.Worker, Attempt: "run-a", Request: researchweb.WorkerRequest{RequestID: "request-a", Epoch: 1, Snapshot: "s", Result: &execution.Result{AttemptID: "run-a", LeaseEpoch: 1, Snapshot: "s"}}}
	if err := saveDelivery(dir, r); err != nil {
		t.Fatal(err)
	}
	wrong := conf
	wrong.Worker = "worker-b"
	if _, err := loadDelivery(dir, wrong); err == nil {
		t.Fatal("delivery sent to another worker")
	}
	body, _ := os.ReadFile(filepath.Join(dir, "result.json"))
	var stored deliveryRecord
	json.Unmarshal(body, &stored)
	stored.Request.Result.Candidate = "changed"
	body, _ = json.Marshal(stored)
	if err := os.WriteFile(filepath.Join(dir, "result.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDelivery(dir, conf); err == nil {
		t.Fatal("changed result trusted")
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDelivery(dir, r); err == nil {
		t.Fatal("public outbox accepted")
	}
}
