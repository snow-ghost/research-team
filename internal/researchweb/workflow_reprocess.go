package researchweb

func (s *Service) workflowReportsReprocessable(d *Data, w *ResearchWorkflow, study string) bool {
	proposal := d.proposal(w.Proposal)
	if proposal == nil || proposal.Status != "applied" || proposal.Target != w.Target {
		return false
	}
	budget := s.observeBudget(d, study)
	if budget.UnreservedUnknown > 0 || budget.UnreservedModelRequests > 0 {
		return false
	}
	if a := d.attempt(w.PlanningAttempt); a == nil || a.Status != "candidate" || a.RemoteOutcome != "response_received" {
		return false
	}
	seen := false
	for _, branch := range d.Branches {
		if branch.Plan != w.Proposal || branch.Team == "" {
			continue
		}
		team := d.team(branch.Team)
		if team == nil {
			return false
		}
		for _, id := range team.Current {
			seen = true
			a := d.attempt(id)
			if a == nil || a.Status != "candidate" || a.RemoteOutcome != "response_received" || a.ResultSHA256 == "" {
				return false
			}
		}
	}
	return seen
}
