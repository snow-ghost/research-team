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
	return missing
}
