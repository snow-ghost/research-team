package researchweb

import (
	"strings"
	"time"

	"github.com/snow-ghost/research-team/internal/execution"
)

const providerFailureThreshold = 3

type ProviderControl struct {
	Key                 string    `json:"key"`
	Profile             string    `json:"profile"`
	Held                bool      `json:"held"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastAttempt         string    `json:"last_attempt,omitempty"`
	LastClass           string    `json:"last_class,omitempty"`
	Note                string    `json:"note,omitempty"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type ProviderCommand struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Profile          string `json:"profile"`
	Kind             string `json:"kind"`
	Note             string `json:"note"`
	Confirm          bool   `json:"confirm"`
}

func providerKey(p execution.Profile) string {
	if p.Model != nil {
		return "endpoint-" + hash(strings.TrimRight(p.Model.BaseURL, "/"))
	}
	return "profile-" + hash(p.ID)
}

func providerHeld(d *Data, p execution.Profile) bool {
	for _, c := range d.ProviderControls {
		if c.Key == providerKey(p) {
			return c.Held
		}
	}
	return false
}

// A timeout or length limit is not evidence that a mathematical claim is false.
func providerFailureClass(r execution.Result) string {
	if r.Status == "cancelled" {
		return "cancelled"
	}
	for i := len(r.Diagnostics) - 1; i >= 0; i-- {
		d := r.Diagnostics[i]
		if d.HTTPStatus == 429 || d.HTTPStatus == 502 || d.HTTPStatus == 503 || d.HTTPStatus == 504 {
			return "availability"
		}
		if d.HTTPStatus == 401 || d.HTTPStatus == 403 {
			return "configuration"
		}
		if d.HTTPStatus >= 200 && d.HTTPStatus < 300 && d.Stage == "response_body" {
			if d.FinishReason == "length" {
				return "output_limit"
			}
			return "response_received"
		}
		if d.Kind != "" && (d.Stage == "connect" || d.Stage == "dns" || d.Stage == "tls") {
			return "availability"
		}
	}
	return "other"
}

func recordProviderOutcome(d *Data, a Attempt, p execution.Profile, r execution.Result) {
	class := providerFailureClass(r)
	if class == "cancelled" || class == "other" {
		return
	}
	key := providerKey(p)
	index := -1
	for i, c := range d.ProviderControls {
		if c.Key == key {
			index = i
			break
		}
	}
	if index < 0 {
		if len(d.ProviderControls) >= 100 {
			return
		}
		d.ProviderControls = append(d.ProviderControls, ProviderControl{Key: key, Profile: p.ID})
		index = len(d.ProviderControls) - 1
	}
	c := &d.ProviderControls[index]
	if c.LastAttempt == a.ID {
		return
	}
	c.LastAttempt, c.LastClass, c.UpdatedAt = a.ID, class, time.Now().UTC()
	if class == "availability" {
		c.ConsecutiveFailures++
		if c.ConsecutiveFailures >= providerFailureThreshold {
			c.Held = true
			c.Note = "Три последовательных отказа доступности; новые передачи удержаны до решения оператора."
		}
	} else {
		c.ConsecutiveFailures = 0
	}
}

func (s *Service) ControlProvider(r ProviderCommand) error {
	p, ok := s.lookupProfile(r.Profile)
	if !ok || !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Note, 4000) || (r.Kind != "pause" && r.Kind != "resume" && r.Kind != "note") {
		return RuleError("Подтвердите действие поставщика и укажите основание.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Управление допуском поставщика", r.Profile, "operator", func(d *Data) error {
		key := providerKey(p)
		index := -1
		for i, c := range d.ProviderControls {
			if c.Key == key {
				index = i
				break
			}
		}
		if index < 0 {
			if len(d.ProviderControls) >= 100 {
				return ErrLimit
			}
			d.ProviderControls = append(d.ProviderControls, ProviderControl{Key: key, Profile: p.ID})
			index = len(d.ProviderControls) - 1
		}
		c := &d.ProviderControls[index]
		c.Note, c.UpdatedAt = r.Note, time.Now().UTC()
		if r.Kind == "pause" {
			c.Held = true
		}
		if r.Kind == "resume" {
			c.Held = false
			c.ConsecutiveFailures = 0
		}
		return nil
	})
}
