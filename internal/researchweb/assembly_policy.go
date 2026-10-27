package researchweb

import "github.com/snow-ghost/research-team/internal/leancheck"

func missingRequiredLemmas(d *Data, e *Entity, pins map[string]string, report *leancheck.Report) []string {
	missing := []string{}
	for _, id := range e.RequiredLibraries {
		found := false
		for _, l := range d.Library {
			if l.ID == id && l.Report != nil && pins[id] == l.Report.ArtifactSHA256 && report != nil && report.Dependencies.Uses(l.Module, l.Goal.Candidate) {
				found = true
			}
		}
		if !found {
			missing = append(missing, id)
		}
	}
	if e.RequireDependencyProofs {
		for _, dep := range requiredProofDependencies(d, e) {
			found := false
			for _, l := range d.Library {
				if l.Lemma == dep && l.Status == "ready" && l.Report != nil && pins[l.ID] == l.Report.ArtifactSHA256 && report != nil && report.Dependencies.Uses(l.Module, l.Goal.Candidate) {
					found = true
				}
			}
			if !found {
				missing = append(missing, "dependency:"+dep)
			}
		}
	}
	return missing
}

func requiredProofDependencies(d *Data, e *Entity) []string {
	if !e.RequireDependencyProofs {
		return nil
	}
	ids := []string{}
	seen := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
		if entity := d.entity(id); entity != nil {
			for _, dep := range entity.Dependencies {
				visit(dep)
			}
		}
	}
	for _, id := range e.Dependencies {
		visit(id)
	}
	return ids
}
