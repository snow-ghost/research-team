package researchweb

func memoryScope(e Entity) string {
	base := hash(struct {
		Statement, Assumptions string
		Goal                   any
		Required               []string
	}{e.Statement, e.Assumptions, e.FormalGoal, e.RequiredLibraries})
	if e.RequireDependencyProofs {
		return hash([]string{base, "dependency-proof-references-required@1"})
	}
	return base
}

func memoryReusable(d *Data, m MemoryEntry) bool {
	if m.Kind == "note" {
		return false
	}
	e := d.entity(m.Target)
	return e != nil && m.ScopeSHA256 != "" && m.ScopeSHA256 == memoryScope(*e) && m.ConditionsSHA256 == hash(m.Conditions) && hash(m.Conditions) == hash(e.Assumptions) && memoryEntryTrust(d, m) != "stale" && memoryEntryTrust(d, m) != "legacy_unscoped"
}

func memoryHit(d *Data, m MemoryEntry, study string) MemoryHit {
	return MemoryHit{ID: m.ID, Target: m.Target, Study: study, Revision: m.TargetRevision, Kind: m.Kind, Content: m.Content, Conditions: m.Conditions, Trust: memoryEntryTrust(d, m), GoalSHA256: m.GoalSHA256, Method: m.Method, FailureClass: m.FailureClass, AutoReuse: memoryReusable(d, m), EvidenceSHA256: m.EvidenceSHA256, Obligations: m.Obligations}
}

func failedAttemptClass(a Attempt) string {
	if a.RemoteOutcome == "unknown" {
		return "unknown_remote_outcome"
	}
	switch a.Status {
	case "limit_reached":
		return "limit_reached"
	case "timed_out":
		return "timeout"
	case "cancelled":
		return "cancelled"
	case "interrupted":
		return "interrupted"
	}
	if a.StopCause == "skill_result_contract" {
		return "format"
	}
	if a.RemoteOutcome == "not_started" {
		return "preparation_failure"
	}
	return "execution_failure"
}
