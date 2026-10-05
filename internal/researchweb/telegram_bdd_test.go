//go:build linux

package researchweb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeTelegram struct {
	mu           sync.Mutex
	updates      []TelegramUpdate
	polls, sends int
	fail         bool
}

func TestTelegramBDD_ReportPreviewRequiresOperatorAndDoesNotSend(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	client := &fakeTelegram{}
	o.TelegramClient = client
	s := serviceFor(t, o)
	v := studyFor(t, s)
	key := strings.Repeat("o", 40)
	h, err := NewHTTP(s, key)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	route := "http://127.0.0.1:4187/api/studies/" + v.Studies[0].ID + "/report"
	request := httptest.NewRequest(http.MethodGet, route, nil)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("preview available without operator authentication")
	}
	request = httptest.NewRequest(http.MethodGet, route, nil)
	request.Header.Set("Authorization", "Bearer "+key)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	var report struct {
		Study    string `json:"study"`
		Revision int    `json:"revision"`
		Text     string `json:"text"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &report) != nil || report.Study != v.Studies[0].ID || report.Revision != v.Revision || report.Text != studyStatus(v.Data, report.Study) {
		t.Fatalf("invalid preview: %d %s", response.Code, response.Body.String())
	}
	after := stateOf(t, s)
	client.mu.Lock()
	defer client.mu.Unlock()
	if after.Revision != v.Revision || len(after.Messages) != 0 || client.sends != 0 || client.polls != 0 {
		t.Fatal("preview performed an external action or changed state")
	}
}

func (f *fakeTelegram) Updates(context.Context, string, int64) ([]TelegramUpdate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	return f.updates, nil
}
func (f *fakeTelegram) Send(context.Context, string, int64, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends++
	if f.fail {
		return errors.New("unknown")
	}
	return nil
}
func TestTelegramBDD_OffByDefaultScopeDedupAndUnknownDelivery(t *testing.T) {
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	channel := TelegramChannel{ID: "chat-test", Label: "Test chat", TokenEnv: "TG_TOKEN", ChatID: 123, AllowedUsers: []int64{7}}
	client := &fakeTelegram{}
	// Configuring the module later does not implicitly connect any study.
	s := serviceFor(t, o)
	s.Options.Config.Telegram = []TelegramChannel{channel}
	lookup := s.Options.Lookup
	s.Options.Lookup = func(key string) (string, bool) {
		if key == "TG_TOKEN" {
			return "test-token", true
		}
		return lookup(key)
	}
	v := studyFor(t, s)
	s.pollTelegram(client, channel)
	if client.polls != 0 {
		t.Fatal("disabled module made a request")
	}
	bind := BindingRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: v.Studies[0].ID, Connector: channel.ID, Enabled: true}
	if err := s.BindTelegram(bind); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"update_id":1,"message":{"text":"unauthorized","chat":{"id":123},"from":{"id":9}}}`, `{"update_id":2,"message":{"text":"Check premise","chat":{"id":123},"from":{"id":7}}}`} {
		var u TelegramUpdate
		_ = json.Unmarshal([]byte(raw), &u)
		client.updates = append(client.updates, u)
	}
	s.pollTelegram(client, channel)
	s.pollTelegram(client, channel)
	v = stateOf(t, s)
	if len(v.Messages) != 1 || len(v.Questions) != 1 || len(v.Tasks) != 1 || len(v.Attempts) != 0 {
		t.Fatal("allowlist, dedup, or no-paid-auto-run violated")
	}
	client.fail = true
	if err := s.SendStudyMessage(MessageRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Study: bind.Study, Connector: channel.ID, StatusReport: true, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	s.pollTelegram(client, channel)
	s.pollTelegram(client, channel)
	v = stateOf(t, s)
	if client.sends != 1 || v.Messages[1].State != "unknown" {
		t.Fatal("unknown delivery was repeated")
	}
	bind.ExpectedRevision = v.Revision
	bind.RequestID = identifier("cmd")
	bind.Enabled = false
	if err := s.BindTelegram(bind); err != nil {
		t.Fatal(err)
	}
	before := client.polls
	s.pollTelegram(client, channel)
	if client.polls != before {
		t.Fatal("disconnected channel polled")
	}
	other := studyFor(t, s)
	bind.ExpectedRevision = other.Revision
	bind.RequestID = identifier("cmd")
	bind.Study = other.Studies[len(other.Studies)-1].ID
	bind.Enabled = true
	if err := s.BindTelegram(bind); err != nil {
		t.Fatal(err)
	}
	firstStudy := other.Studies[0].ID
	bind.ExpectedRevision = stateOf(t, s).Revision
	bind.RequestID = identifier("cmd")
	bind.Study = firstStudy
	if err := s.BindTelegram(bind); err == nil {
		t.Fatal("chat connected to two studies")
	}
}
