package researchweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

type ConnectorBinding struct {
	ID        string `json:"id"`
	Study     string `json:"study"`
	Kind      string `json:"kind"`
	Connector string `json:"connector"`
	Enabled   bool   `json:"enabled"`
}
type BindingRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Study            string `json:"study"`
	Connector        string `json:"connector"`
	Enabled          bool   `json:"enabled"`
}
type TelegramChannel struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	TokenEnv     string  `json:"token_env"`
	ChatID       int64   `json:"chat_id"`
	AllowedUsers []int64 `json:"allowed_user_ids"`
}
type TelegramOffset struct {
	ID   string `json:"id"`
	Next int64  `json:"next"`
}
type StudyMessage struct {
	ID        string    `json:"id"`
	Study     string    `json:"study"`
	Connector string    `json:"connector"`
	Direction string    `json:"direction"`
	Text      string    `json:"text"`
	Question  string    `json:"question,omitempty"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}
type TelegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}
type TelegramClient interface {
	Updates(context.Context, string, int64) ([]TelegramUpdate, error)
	Send(context.Context, string, int64, string) error
}
type telegramHTTP struct{}

func (telegramHTTP) call(ctx context.Context, token, method string, body, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", "https://api.telegram.org/bot"+token+"/"+method, bytes.NewReader(data))
	if err != nil {
		return errors.New("telegram request unavailable")
	}
	request.Header.Set("Content-Type", "application/json")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("telegram transport failed; delivery unknown")
	}
	defer response.Body.Close()
	data, err = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || response.StatusCode != 200 {
		return errors.New("telegram response unavailable")
	}
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(data, &envelope) != nil || !envelope.OK {
		return errors.New("telegram request rejected")
	}
	if result != nil {
		return json.Unmarshal(envelope.Result, result)
	}
	return nil
}
func (c telegramHTTP) Updates(ctx context.Context, token string, offset int64) ([]TelegramUpdate, error) {
	var rows []TelegramUpdate
	err := c.call(ctx, token, "getUpdates", map[string]any{"offset": offset, "limit": 20, "timeout": 0, "allowed_updates": []string{"message"}}, &rows)
	return rows, err
}
func (c telegramHTTP) Send(ctx context.Context, token string, chat int64, text string) error {
	return c.call(ctx, token, "sendMessage", map[string]any{"chat_id": chat, "text": text}, nil)
}
func validateTelegram(channels []TelegramChannel) error {
	if len(channels) > 20 {
		return errors.New("too many telegram channels")
	}
	seen := map[string]bool{}
	for _, c := range channels {
		if !requestPattern.MatchString(c.ID) || seen[c.ID] || c.TokenEnv == "" || c.ChatID == 0 || len(c.AllowedUsers) == 0 || len(c.AllowedUsers) > 100 {
			return errors.New("invalid telegram channel")
		}
		for _, id := range c.AllowedUsers {
			if id <= 0 {
				return errors.New("invalid telegram user")
			}
		}
		seen[c.ID] = true
	}
	return nil
}
func (s *Service) BindTelegram(r BindingRequest) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны снимок и идентификатор команды.")
	}
	configured := false
	for _, c := range s.Options.Config.Telegram {
		if c.ID == r.Connector {
			configured = true
		}
	}
	if !configured {
		return RuleError("Канал Telegram не настроен администратором.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Настройка Telegram", r.Study, "operator", func(d *Data) error {
		found := false
		for _, study := range d.Studies {
			if study.ID == r.Study {
				found = true
			}
		}
		if !found {
			return RuleError("Исследование не найдено.")
		}
		for _, b := range d.Bindings {
			if b.Kind != "telegram" || b.Connector != r.Connector {
				continue
			}
			if r.Enabled && b.Enabled && b.Study != r.Study {
				return RuleError("Чат уже подключен к другому исследованию.")
			}
		}
		for i, b := range d.Bindings {
			if b.Kind == "telegram" && b.Connector == r.Connector && b.Study == r.Study {
				d.Bindings[i].Enabled = r.Enabled
				return nil
			}
		}
		d.Bindings = append(d.Bindings, ConnectorBinding{ID: identifier("binding"), Study: r.Study, Kind: "telegram", Connector: r.Connector, Enabled: r.Enabled})
		return nil
	})
}

type MessageRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Study            string `json:"study"`
	Connector        string `json:"connector"`
	Text             string `json:"text"`
	StatusReport     bool   `json:"status_report"`
	Confirm          bool   `json:"confirm"`
}

func (s *Service) SendStudyMessage(r MessageRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Подтвердите отправку сообщения.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Сообщение Telegram", r.Study, "operator", func(d *Data) error {
		if !telegramEnabled(*d, r.Study, r.Connector) {
			return RuleError("Канал выключен для исследования.")
		}
		text := r.Text
		if r.StatusReport {
			text = studyStatus(*d, r.Study)
		}
		if !textOK(text, 4000) {
			return RuleError("Нужен текст до 4000 байт.")
		}
		d.Messages = append(d.Messages, StudyMessage{ID: identifier("message"), Study: r.Study, Connector: r.Connector, Direction: "outgoing", Text: text, State: "queued", CreatedAt: time.Now().UTC()})
		return nil
	})
}
func telegramEnabled(d Data, study, channel string) bool {
	for _, b := range d.Bindings {
		if b.Kind == "telegram" && b.Study == study && b.Connector == channel && b.Enabled {
			return true
		}
	}
	return false
}
func studyStatus(d Data, id string) string {
	for _, study := range d.Studies {
		if study.ID == id {
			counts := map[string]int{}
			for _, e := range d.Entities {
				if e.Study == id {
					counts[d.effective(e.ID, map[string]bool{})]++
				}
			}
			return execution.Preview(fmt.Sprintf("%s\nСнимок: %d\nПринято: %d; открыто: %d; на проверке: %d; требует исправлений: %d.", study.Title, d.Revision, counts["accepted"], counts["open"], counts["in_review"], counts["needs_changes"]), 4000)
		}
	}
	return "Исследование не найдено."
}
func (s *Service) telegramLoop() {
	defer s.wg.Done()
	client := s.Options.TelegramClient
	if client == nil {
		client = telegramHTTP{}
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// A write interrupted by restart is not retried without operator reconciliation.
	if err := s.Store.Change(0, "", "", "Восстановление сообщений", "", "server", func(d *Data) error {
		changed := false
		for i := range d.Messages {
			if d.Messages[i].State == "sending" {
				d.Messages[i].State = "unknown"
				changed = true
			}
		}
		if !changed {
			return errNoCycleChange
		}
		return nil
	}); err != nil && !errors.Is(err, errNoCycleChange) {
		s.cancel()
		return
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			for _, channel := range s.Options.Config.Telegram {
				s.pollTelegram(client, channel)
			}
		}
	}
}
func (s *Service) pollTelegram(client TelegramClient, c TelegramChannel) {
	view, err := s.Store.Read()
	if err != nil {
		return
	}
	study := ""
	for _, b := range view.Bindings {
		if b.Kind == "telegram" && b.Connector == c.ID && b.Enabled {
			study = b.Study
		}
	}
	if study == "" {
		return
	}
	token, ok := s.Options.Lookup(c.TokenEnv)
	if !ok || token == "" || strings.ContainsAny(token, "/\r\n") {
		return
	}
	offset := int64(0)
	for _, o := range view.TelegramOffsets {
		if o.ID == c.ID {
			offset = o.Next
		}
	}
	rows, err := client.Updates(s.ctx, token, offset)
	if err == nil && len(rows) > 0 {
		err = s.Store.Change(0, "", "", "Входящие сообщения", study, "telegram", func(d *Data) error {
			if !telegramEnabled(*d, study, c.ID) {
				return errNoCycleChange
			}
			current := int64(0)
			index := -1
			for i, o := range d.TelegramOffsets {
				if o.ID == c.ID {
					current = o.Next
					index = i
				}
			}
			changed := false
			for _, u := range rows {
				if u.UpdateID < current || u.UpdateID < offset || u.UpdateID < 0 {
					continue
				}
				current = u.UpdateID + 1
				changed = true
				if u.Message == nil || u.Message.Chat.ID != c.ChatID || !slices.Contains(c.AllowedUsers, u.Message.From.ID) || !textOK(u.Message.Text, 4000) {
					continue
				}
				text := strings.ReplaceAll(u.Message.Text, token, "[REDACTED]")
				if text == "/status" {
					d.Messages = append(d.Messages, StudyMessage{ID: identifier("message"), Study: study, Connector: c.ID, Direction: "outgoing", Text: studyStatus(*d, study), State: "queued", CreatedAt: time.Now().UTC()})
					continue
				}
				goal := ""
				for _, st := range d.Studies {
					if st.ID == study {
						goal = st.Goal
					}
				}
				id := identifier("Q")
				d.Questions = append(d.Questions, Question{ID: id, Target: goal, Text: text, Kind: "question", Snapshot: d.Revision + 1})
				d.addTask(goal, "Ответ на вопрос Telegram", "answer", text)
				d.Tasks[len(d.Tasks)-1].Question = id
				d.Messages = append(d.Messages, StudyMessage{ID: identifier("message"), Study: study, Connector: c.ID, Direction: "incoming", Text: text, Question: id, State: "received", CreatedAt: time.Now().UTC()})
			}
			if !changed {
				return errNoCycleChange
			}
			if index >= 0 {
				d.TelegramOffsets[index].Next = current
			} else {
				d.TelegramOffsets = append(d.TelegramOffsets, TelegramOffset{ID: c.ID, Next: current})
			}
			return nil
		})
		if err != nil && !errors.Is(err, errNoCycleChange) {
			s.cancel()
			return
		}
	}
	view, err = s.Store.Read()
	if err != nil {
		return
	}
	for _, m := range view.Messages {
		if m.Connector != c.ID || m.State != "queued" || !telegramEnabled(view.Data, m.Study, c.ID) {
			continue
		}
		err = s.Store.Change(0, "", "", "Намерение отправить сообщение", m.Study, "telegram", func(d *Data) error {
			if !telegramEnabled(*d, m.Study, c.ID) {
				return errNoCycleChange
			}
			for i := range d.Messages {
				if d.Messages[i].ID == m.ID && d.Messages[i].State == "queued" {
					d.Messages[i].State = "sending"
					return nil
				}
			}
			return errNoCycleChange
		})
		if err != nil {
			return
		}
		sendErr := client.Send(s.ctx, token, c.ChatID, m.Text)
		if err = s.Store.Change(0, "", "", "Исход отправки сообщения", m.Study, "telegram", func(d *Data) error {
			for i := range d.Messages {
				if d.Messages[i].ID == m.ID {
					d.Messages[i].State = "sent"
					if sendErr != nil {
						d.Messages[i].State = "unknown"
					}
					return nil
				}
			}
			return errNoCycleChange
		}); err != nil {
			s.cancel()
		}
		break
	}
}
