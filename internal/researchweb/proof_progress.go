package researchweb

type ProofProgressNode struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	Native       string   `json:"native"`
	Dependencies []string `json:"dependencies"`
	Blockers     []string `json:"blockers"`
	Modules      []string `json:"modules"`
}

type ProofProgress struct {
	Revision int                 `json:"revision"`
	Study    string              `json:"study"`
	Goal     string              `json:"goal"`
	Nodes    []ProofProgressNode `json:"nodes"`
}

func (s *Service) ProofProgress(study string) (ProofProgress, error) {
	v, err := s.Store.Read()
	if err != nil {
		return ProofProgress{}, err
	}
	st := v.study(study)
	if st == nil {
		return ProofProgress{}, RuleError("Исследование не найдено.")
	}
	out := ProofProgress{Revision: v.Revision, Study: study, Goal: st.Goal, Nodes: []ProofProgressNode{}}
	for _, e := range v.Entities {
		if e.Study != study {
			continue
		}
		n := ProofProgressNode{ID: e.ID, Title: e.Title, Kind: e.Kind, Status: v.effective(e.ID, map[string]bool{}), Native: "none", Dependencies: e.Dependencies, Blockers: []string{}, Modules: []string{}}
		if e.FormalGoal == nil {
			n.Blockers = append(n.Blockers, "Не закреплена формальная цель.")
		}
		for _, dep := range e.Dependencies {
			if v.effective(dep, map[string]bool{}) != "accepted" {
				title := dep
				if x := v.entity(dep); x != nil {
					title = x.Title
				}
				n.Blockers = append(n.Blockers, "Не принято основание: "+title)
			}
		}
		for _, f := range v.Findings {
			if f.Target == e.ID && f.State != "resolved" && f.Severity != "editorial" {
				n.Blockers = append(n.Blockers, "Незакрытое замечание: "+f.Text)
			}
		}
		for _, check := range v.Verifications {
			if check.Target != e.ID || check.Purpose == "refutation" {
				continue
			}
			if verifiedReportMatches(check) && verificationMatches(&v.Data, check) {
				n.Native = "verified"
			} else if n.Native != "verified" {
				n.Native = check.Status
			}
		}
		for _, l := range v.Library {
			if l.Lemma == e.ID && l.Status == "ready" && libraryMatches(&v.Data, l) {
				n.Modules = append(n.Modules, l.Module)
			}
		}
		out.Nodes = append(out.Nodes, n)
	}
	return out, nil
}
