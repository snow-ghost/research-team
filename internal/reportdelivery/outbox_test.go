//go:build linux

package reportdelivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const testToken = "local-operator-credential-for-test-0001"

func TestOutboxBDD_ProducerRestartReusesImmutableReportKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "outbox")
	note := Note{Target: "H-1", Content: "Completed report", Conditions: "Technical note"}
	first, err := QueueOnce(dir, "experiment-terminal-001", note)
	if err != nil {
		t.Fatal(err)
	}
	second, err := QueueOnce(dir, "experiment-terminal-001", note)
	if err != nil || first.ID != second.ID || first.CreatedAt != second.CreatedAt {
		t.Fatal("producer replay created another report", err)
	}
	note.Content = "Changed content"
	if _, err = QueueOnce(dir, "experiment-terminal-001", note); err == nil {
		t.Fatal("report identity reused with different content")
	}
}

func TestOutboxBDD_AmbiguousCommittedDeliveryReusesExactCommandWithoutDuplicate(t *testing.T) {
	posts, mutations, revision := 0, 0, 1
	commands := map[string]string{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Error("missing credential")
		}
		if r.Method == "GET" {
			fmt.Fprintf(w, `{"state":{"revision":%d}}`, revision)
			return
		}
		posts++
		var payload map[string]any
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			t.Error("bad payload")
		}
		id := payload["request_id"].(string)
		fingerprint := digest(payload)
		if old, ok := commands[id]; ok {
			if old != fingerprint {
				t.Error("ambiguous request rebased")
			}
		} else {
			commands[id] = fingerprint
			mutations++
			revision++
		}
		if posts == 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		fmt.Fprintf(w, `{"state":{"revision":%d}}`, revision)
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "outbox")
	e, err := Queue(dir, Note{Target: "H-example", Content: "Technical report", Conditions: "No mathematical acceptance"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(server.URL, testToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.Deliver(context.Background(), dir, e.ID)
	mu.Lock()
	m := mutations
	mu.Unlock()
	if !errors.Is(err, ErrPending) || first.Status != "unknown" || first.ExpectedRevision != 1 || m != 1 {
		t.Fatal("ambiguous outcome lost", first, err)
	}
	second, err := client.Deliver(context.Background(), dir, e.ID)
	mu.Lock()
	p, m := posts, mutations
	mu.Unlock()
	if err != nil || second.Status != "delivered" || p != 2 || m != 1 || second.ExpectedRevision != 1 {
		t.Fatal("delivery duplicated", second, err)
	}
	_, err = client.Deliver(context.Background(), dir, e.ID)
	mu.Lock()
	p = posts
	mu.Unlock()
	if err != nil || p != 2 {
		t.Fatal("acknowledged command sent again", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, e.ID+".json"))
	if strings.Contains(string(body), testToken) {
		t.Fatal("credential persisted")
	}
}

func TestOutboxBDD_ExplicitConflictAllowsRefreshButKeepsIdentity(t *testing.T) {
	posts, reads := 0, 0
	ids := []string{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "GET" {
			reads++
			fmt.Fprintf(w, `{"state":{"revision":%d}}`, reads)
			return
		}
		posts++
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		ids = append(ids, b["request_id"].(string))
		if posts == 1 {
			w.WriteHeader(409)
			return
		}
		fmt.Fprint(w, `{"state":{"revision":3}}`)
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "outbox")
	e, err := Queue(dir, Note{Target: "H", Content: "Report"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := NewClient(server.URL, testToken, server.Client())
	e, err = c.Deliver(context.Background(), dir, e.ID)
	if !errors.Is(err, ErrPending) || e.ExpectedRevision != 0 {
		t.Fatal(e, err)
	}
	e, err = c.Deliver(context.Background(), dir, e.ID)
	mu.Lock()
	same := len(ids) == 2 && ids[0] == ids[1]
	n := reads
	mu.Unlock()
	if err != nil || e.Status != "delivered" || n != 2 || !same {
		t.Fatal("identity reset", e, err)
	}
}

func TestOutboxBDD_RedirectsAndRemoteServersCannotReceiveCredentials(t *testing.T) {
	if _, err := NewClient("https://example.com", testToken, nil); err == nil {
		t.Fatal("remote delivery allowed")
	}
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"state":{"revision":1}}`)
			return
		}
		http.Redirect(w, r, target.URL, 302)
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "outbox")
	e, err := Queue(dir, Note{Target: "H", Content: "Report"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := NewClient(server.URL, testToken, server.Client())
	_, err = c.Deliver(context.Background(), dir, e.ID)
	if !errors.Is(err, ErrPending) || forwarded.Load() != 0 {
		t.Fatal("credential redirect followed", err)
	}
}
