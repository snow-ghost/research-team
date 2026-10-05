package researchweb

import "github.com/snow-ghost/research-team/internal/execution"

type Observation struct {
	Budget           BudgetObservation `json:"budget"`
	Study            string            `json:"study"`
	Revision         int               `json:"revision"`
	Attempts         int               `json:"attempts"`
	Completed        int               `json:"completed"`
	Active           int               `json:"active"`
	Metered          int               `json:"metered"`
	UnknownUsage     int               `json:"unknown_usage"`
	InputTokens      int64             `json:"input_tokens"`
	OutputTokens     int64             `json:"output_tokens"`
	Teams            []ResearchTeam    `json:"teams"`
	Results          []ResearchResult  `json:"results"`
	PendingQuestions []Question        `json:"pending_questions"`
	Findings         []Finding         `json:"findings"`
}

func (s *Service) ObserveStudy(id string) (Observation, error) {
	v, err := s.Store.Read()
	if err != nil {
		return Observation{}, err
	}
	found := false
	for _, study := range v.Studies {
		if study.ID == id {
			found = true
		}
	}
	if !found {
		return Observation{}, RuleError("Исследование не найдено.")
	}
	o := Observation{Study: id, Revision: v.Revision, Teams: []ResearchTeam{}, Results: []ResearchResult{}, PendingQuestions: []Question{}, Findings: []Finding{}}
	o.Budget = s.observeBudget(&v.Data, id)
	for _, a := range v.Attempts {
		e := v.entity(a.Target)
		if e == nil || e.Study != id {
			continue
		}
		o.Attempts++
		if active(a.Status) {
			o.Active++
			continue
		}
		o.Completed++
		r, err := s.readResult(a)
		if err != nil || r.Usage == nil || r.Usage.Incomplete {
			o.UnknownUsage++
			continue
		}
		o.Metered++
		o.InputTokens += r.Usage.InputTokens
		o.OutputTokens += r.Usage.OutputTokens
	}
	for _, t := range v.Teams {
		if t.Study == id {
			o.Teams = append(o.Teams, t)
		}
	}
	for _, r := range v.Results {
		if e := v.entity(r.Target); e != nil && e.Study == id {
			o.Results = append(o.Results, r)
		}
	}
	for _, q := range v.Questions {
		if e := v.entity(q.Target); e != nil && e.Study == id && q.Kind != "proposal" && q.Answer == "" {
			q.Text = execution.Preview(q.Text, 4000)
			o.PendingQuestions = append(o.PendingQuestions, q)
		}
	}
	for _, f := range v.Findings {
		if e := v.entity(f.Target); e != nil && e.Study == id && f.State != "resolved" {
			o.Findings = append(o.Findings, f)
		}
	}
	return o, nil
}
