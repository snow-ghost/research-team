package researchweb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"time"

	"github.com/snow-ghost/research-team/internal/coddy"
)

type PrepareCoddy struct {
	ExpectedRevision int      `json:"expected_revision"`
	RequestID        string   `json:"request_id"`
	TaskID           string   `json:"task_id"`
	SourceCommit     string   `json:"source_commit"`
	Acceptance       []string `json:"acceptance"`
	Profile          string   `json:"profile,omitempty"`
}
type CoddyCommand struct {
	Kind         string               `json:"kind"`
	RequestID    string               `json:"request_id"`
	Target       string               `json:"target,omitempty"`
	ExpectedView string               `json:"expected_view,omitempty"`
	CommentID    int64                `json:"comment_id,omitempty"`
	Publish      bool                 `json:"publish"`
	Body         string               `json:"body,omitempty"`
	Review       *coddy.ReviewRequest `json:"review,omitempty"`
}

func (s *Service) connector() (*coddy.Connector, error) {
	if s.Options.Coddy == nil {
		return nil, RuleError("Coddy Bot не настроен на сервере.")
	}
	store, err := coddy.OpenStore(filepath.Join(s.Options.Config.DataDir, "coddy"))
	if err != nil {
		return nil, err
	}
	return coddy.New(*s.Options.Coddy, store, s.Options.Lookup)
}
func (s *Service) PrepareCoddy(r PrepareCoddy) error {
	if r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) {
		return RuleError("Нужны версия снимка и идентификатор команды.")
	}
	c, err := s.connector()
	if err != nil {
		return err
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Подготовлено делегирование Coddy", r.TaskID, "operator", func(d *Data) error {
		t := d.task(r.TaskID)
		if t == nil {
			return RuleError("Задание не найдено.")
		}
		item := d.entity(t.Target)
		job := coddy.Job{TaskID: t.ID, AttemptID: "coddy-" + r.RequestID, InputSnapshot: reference(d.Revision),
			LeaseEpoch: 1, Title: t.Title, Objective: t.Objective, Context: item.Statement + "\nУсловия: " + item.Assumptions,
			SourceCommit: r.SourceCommit, Acceptance: r.Acceptance}
		if r.Profile != "" {
			p, ok := s.Options.Profiles[r.Profile]
			if !ok {
				return RuleError("Профиль навыков не найден.")
			}
			job.Skills = p.Skills
		}
		record, err := c.Prepare(job)
		if err != nil {
			return err
		}
		for _, existing := range d.Delegations {
			if existing.ID == record.ID {
				return nil
			}
		}
		d.Delegations = append(d.Delegations, Delegation{ID: record.ID, Target: item.ID})
		return nil
	})
}
func (s *Service) knownDelegation(id string) error {
	v, err := s.Store.Read()
	if err != nil {
		return err
	}
	for _, d := range v.Delegations {
		if d.ID == id {
			return nil
		}
	}
	return RuleError("Делегирование не найдено в этой рабочей области.")
}
func (s *Service) CoddyLocal(id string) (*coddy.Record, error) {
	if err := s.knownDelegation(id); err != nil {
		return nil, err
	}
	c, err := s.connector()
	if err != nil {
		return nil, err
	}
	return c.Local(id)
}
func (s *Service) CoddyCommand(ctx context.Context, id string, r CoddyCommand) (*coddy.Record, error) {
	if err := s.knownDelegation(id); err != nil {
		return nil, err
	}
	c, err := s.connector()
	if err != nil {
		return nil, err
	}
	if !requestPattern.MatchString(r.RequestID) {
		return nil, RuleError("Нужен идентификатор команды.")
	}
	ctx, stop := context.WithTimeout(ctx, 2*time.Minute)
	defer stop()
	switch r.Kind {
	case "submit":
		return c.Submit(ctx, id, r.Publish)
	case "observe":
		return c.Observe(ctx, id)
	case "reconcile":
		return c.Reconcile(ctx, id)
	case "hold":
		return c.Hold(id, true)
	case "resume":
		return c.Hold(id, false)
	case "approve-plan":
		return c.ApprovePlan(ctx, id, r.RequestID, r.Target, r.ExpectedView, r.CommentID, r.Publish)
	case "reply":
		return c.Reply(ctx, id, r.RequestID, r.Target, r.ExpectedView, r.Body, r.Publish)
	case "request-changes":
		if r.Review == nil {
			return nil, RuleError("Нужна рецензия с замечаниями к строкам.")
		}
		return c.RequestChanges(ctx, id, r.RequestID, r.ExpectedView, *r.Review, r.Publish)
	case "collect":
		return c.Collect(ctx, id, r.ExpectedView)
	default:
		return nil, RuleError("Неизвестная команда Coddy.")
	}
}
func (s *Service) CoddyCandidate(id, sha string) ([]byte, error) {
	r, err := s.CoddyLocal(id)
	if err != nil {
		return nil, err
	}
	found := false
	for _, candidate := range r.Candidates {
		found = found || candidate == sha
	}
	if !found {
		return nil, RuleError("Кандидат не принадлежит делегированию.")
	}
	path := filepath.Join(s.Options.Config.DataDir, "coddy", "candidate-"+sha+".json")
	data, err := readBoundedFile(path, 32<<20)
	if err != nil || len(data) > 32<<20 {
		return nil, errors.New("candidate unavailable")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != sha {
		return nil, errors.New("candidate integrity check failed")
	}
	return data, nil
}
