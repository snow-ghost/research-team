//go:build linux

package researchweb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-ghost/research-team/internal/reportdelivery"
)

func TestDeliveryBDD_CommitThenLostAcknowledgementDoesNotDuplicateMemoryAcrossRestart(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			s, target := submittedService(t, backend, "verified")
			key := strings.Repeat("d", 64)
			h, err := NewHTTP(s, key)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { h.Close() }()
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Host = "127.0.0.1:4187"
				if r.Method == "POST" && r.URL.Path == "/api/memory" {
					posts++
					if posts == 1 {
						record := httptest.NewRecorder()
						h.ServeHTTP(record, r)
						if record.Code != 200 {
							t.Errorf("first commit failed: %d", record.Code)
						}
						conn, _, _ := w.(http.Hijacker).Hijack()
						conn.Close()
						return
					}
				}
				h.ServeHTTP(w, r)
			}))
			defer server.Close()
			dir := filepath.Join(t.TempDir(), "outbox")
			entry, err := reportdelivery.Queue(dir, reportdelivery.Note{Target: target, Content: "Completed local experiment; no acceptance", Conditions: "Only a technical report"})
			if err != nil {
				t.Fatal(err)
			}
			client, err := reportdelivery.NewClient(server.URL, key, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.Deliver(context.Background(), dir, entry.ID); !errors.Is(err, reportdelivery.ErrPending) {
				t.Fatal("lost acknowledgement not retained", err)
			}
			if len(stateOf(t, s).Memory) != 1 {
				t.Fatal("first commit missing")
			}
			h.Close()
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s = serviceFor(t, s.Options)
			h, err = NewHTTP(s, key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.Deliver(context.Background(), dir, entry.ID); err != nil {
				t.Fatal(err)
			}
			if posts != 2 || len(stateOf(t, s).Memory) != 1 {
				t.Fatal("memory duplicated after restart")
			}
		})
	}
}
