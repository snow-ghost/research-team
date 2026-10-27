package researchweb

// Called only for reachable modules that passed libraryMatches, including accepted coverage.
func libraryModuleMaterial(e Entity, l LibraryEntry) map[string]any {
	return map[string]any{
		"module": l.Module, "lemma": l.Lemma, "revision": l.LemmaRevision,
		"declaration": l.Goal.Declaration, "candidate": l.Goal.Candidate,
		"artifact_sha256": l.Report.ArtifactSHA256, "goal": l.Goal,
		"kind": e.Kind, "acceptance_status": "accepted",
		"statement": e.Statement, "assumptions": e.Assumptions, "result": l.Result,
	}
}
