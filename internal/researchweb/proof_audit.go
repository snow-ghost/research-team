package researchweb

import "github.com/snow-ghost/research-team/internal/leancheck"

type ProofCheckView struct {
	MissingRequiredLibraries []string          `json:"missing_required_libraries"`
	DependencyAuditCurrent   bool              `json:"dependency_audit_current"`
	ID                       string            `json:"id"`
	Attempt                  string            `json:"attempt,omitempty"`
	Purpose                  string            `json:"purpose,omitempty"`
	Status                   string            `json:"status"`
	Current                  bool              `json:"current"`
	Source                   string            `json:"source"`
	Goal                     leancheck.Goal    `json:"goal"`
	Report                   *leancheck.Report `json:"report,omitempty"`
	Lemmas                   []LemmaUse        `json:"lemmas"`
}

type LemmaUse struct {
	Library     string `json:"library"`
	Target      string `json:"target"`
	Title       string `json:"title"`
	Module      string `json:"module"`
	Declaration string `json:"declaration"`
	Required    bool   `json:"required"`
	Referenced  bool   `json:"referenced"`
}

type BranchTrace struct {
	ID     string       `json:"id"`
	Status string       `json:"status"`
	Reason string       `json:"reason"`
	Basis  *MethodBasis `json:"basis,omitempty"`
}

type AttemptTrace struct {
	ID               string   `json:"id"`
	Role             string   `json:"role"`
	Status           string   `json:"status"`
	RemoteOutcome    string   `json:"remote_outcome"`
	StopCause        string   `json:"stop_cause,omitempty"`
	MissingMaterials []string `json:"missing_materials,omitempty"`
}

func proofAudit(d *Data, e Entity) ([]ProofCheckView, []BranchTrace, []AttemptTrace) {
	checks, branches, attempts := []ProofCheckView{}, []BranchTrace{}, []AttemptTrace{}
	for _, v := range d.Verifications {
		if v.Target != e.ID {
			continue
		}
		x := ProofCheckView{ID: v.ID, Attempt: v.Attempt, Purpose: v.Purpose, Status: v.Status, Current: verifiedReportMatches(v) && verificationMatches(d, v), Source: v.Source, Goal: v.Goal, Report: v.Report, Lemmas: []LemmaUse{}}
		x.DependencyAuditCurrent = v.Report != nil && v.Report.Dependencies != nil && v.Report.Dependencies.ProcedureSHA256 == leancheck.DependencyDigest()
		x.MissingRequiredLibraries = []string{}
		if v.Purpose != "refutation" {
			x.MissingRequiredLibraries = missingRequiredLemmas(d, &e, v.LibraryPins, v.Report)
		}
		for _, l := range d.Library {
			if _, pinned := v.LibraryPins[l.ID]; !pinned {
				continue
			}
			title := l.Lemma
			if lemma := d.entity(l.Lemma); lemma != nil {
				title = lemma.Title
			}
			use := LemmaUse{Library: l.ID, Target: l.Lemma, Title: title, Module: l.Module, Declaration: l.Goal.Candidate}
			for _, id := range e.RequiredLibraries {
				use.Required = use.Required || id == l.ID
			}
			for _, id := range requiredProofDependencies(d, &e) {
				use.Required = use.Required || id == l.Lemma
			}
			if v.Report != nil {
				use.Referenced = v.Report.Dependencies.Uses(l.Module, l.Goal.Candidate)
			}
			x.Lemmas = append(x.Lemmas, use)
		}
		checks = append(checks, x)
	}
	for _, b := range d.Branches {
		if b.Target == e.ID {
			branches = append(branches, BranchTrace{ID: b.ID, Status: b.Status, Reason: b.Reason, Basis: b.Basis})
		}
	}
	for _, a := range d.Attempts {
		if a.Target != e.ID {
			continue
		}
		role := a.Role
		if t := d.task(a.TaskID); role == "" && t != nil {
			role = t.Kind
		}
		attempts = append(attempts, AttemptTrace{ID: a.ID, Role: role, Status: a.Status, RemoteOutcome: a.RemoteOutcome, StopCause: a.StopCause, MissingMaterials: a.MissingMaterials})
	}
	return checks, branches, attempts
}
