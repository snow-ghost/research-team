package researchweb

import (
	"context"
	"slices"
	"strings"
	"time"
)

type CatalogSearchRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Target           string `json:"target"`
	Query            string `json:"query,omitempty"`
	Limit            int    `json:"limit"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
	Confirm          bool   `json:"confirm"`
}
type CatalogSearchResult struct {
	Revision   int                `json:"revision"`
	Matches    []NativeLemmaMatch `json:"matches"`
	Candidates int                `json:"candidates"`
	Status     string             `json:"status"`
}

func catalogCandidates(d *Data, target, query string) []string {
	ids := []string{}
	query = strings.ToLower(strings.TrimSpace(query))
	for _, l := range d.Library {
		e := d.entity(l.Lemma)
		if e == nil || l.Lemma == target || l.Status != "ready" || !libraryMatches(d, l) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(e.Title+" "+e.Statement+" "+l.Module), query) {
			continue
		}
		ids = append(ids, l.ID)
	}
	slices.Sort(ids)
	return ids
}

func (s *Service) SearchCatalog(ctx context.Context, r CatalogSearchRequest) (CatalogSearchResult, error) {
	out := CatalogSearchResult{Matches: []NativeLemmaMatch{}, Status: "complete"}
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || r.Limit < 1 || r.Limit > 8 || r.TimeoutSeconds < 1 || r.TimeoutSeconds > 120 || len(r.Query) > 200 {
		return out, RuleError("Подтвердите предел: 1..8 лемм и 1..120 секунд.")
	}
	view, err := s.Store.Read()
	if err != nil {
		return out, err
	}
	group := hash(r)
	out.Candidates = len(catalogCandidates(&view.Data, r.Target, r.Query))
	for _, m := range view.LemmaMatches {
		if m.CatalogRequest == group {
			if m.Target != r.Target {
				return out, ErrConflict
			}
			if !matchCurrent(&view.Data, m) {
				return out, ErrConflict
			}
			out.Matches = append(out.Matches, m)
		}
	}
	if len(out.Matches) > 0 {
		out.Revision = view.Revision
		out.Status = "recorded"
		return out, nil
	}
	if view.Revision != r.ExpectedRevision {
		return out, ErrConflict
	}
	target := view.entity(r.Target)
	if target == nil || target.FormalGoal == nil || studyClosed(&view.Data, target.Study) {
		return out, RuleError("Нужна открытая формальная цель исследования.")
	}
	version := target.Revision
	candidates := catalogCandidates(&view.Data, r.Target, r.Query)
	out.Candidates = len(candidates)
	run, cancel := context.WithTimeout(ctx, time.Duration(r.TimeoutSeconds)*time.Second)
	defer cancel()
	for i, id := range candidates {
		if i >= r.Limit || run.Err() != nil {
			out.Status = "bounded"
			break
		}
		current, err := s.Store.Read()
		if err != nil {
			return out, err
		}
		if current.entity(r.Target) == nil || current.entity(r.Target).Revision != version {
			return out, ErrConflict
		}
		err = s.MatchLemma(run, MatchLemmaRequest{ExpectedRevision: current.Revision, RequestID: hash([]string{group, id}), Target: r.Target, Library: id, CatalogRequest: group})
		if err != nil {
			return out, err
		}
		current, err = s.Store.Read()
		if err != nil {
			return out, err
		}
		var m NativeLemmaMatch
		for _, candidate := range current.LemmaMatches {
			if candidate.CatalogRequest == group && candidate.Library == id {
				m = candidate
			}
		}
		if m.ID == "" {
			return out, ErrConflict
		}
		out.Matches = append(out.Matches, m)
		out.Revision = current.Revision
		if m.Report.Status == "verified" {
			out.Status = "verified"
			break
		}
	}
	if out.Revision == 0 {
		out.Revision = view.Revision
	}
	return out, nil
}
