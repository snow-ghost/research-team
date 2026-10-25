package researchweb

import "testing"

func TestCompletionBDD_AcceptedRootStopsOtherAttemptsAndKeepsUnknownOutcome(t *testing.T) {
	d := emptyData()
	d.Studies = append(d.Studies, Study{ID: "study", Goal: "root"})
	root := newEntity("root", "Root", "goal", "study", "True", "Prop")
	root.Status = "accepted"
	other := newEntity("child", "Child", "lemma", "study", "True", "Prop")
	d.Entities = append(d.Entities, root, other)
	d.Attempts = append(d.Attempts, Attempt{ID: "queued", Target: "child", TaskID: "q", Status: "queued", RemoteOutcome: "not_started"}, Attempt{ID: "running", Target: "child", TaskID: "r", Status: "running", RemoteOutcome: "unknown"}, Attempt{ID: "remote", Target: "child", TaskID: "w", Status: "awaiting_worker", RemoteWorker: "worker", RemoteOutcome: "unknown"}, Attempt{ID: "history", Target: "child", Status: "candidate", ResultSHA256: "original"})
	stopClosedStudies(&d)
	if d.Attempts[0].Status != "cancelled" || d.Attempts[0].RemoteOutcome != "not_started" || d.Attempts[1].Status != "cancelling" || d.Attempts[2].Status != "cancelled" || d.Attempts[2].RemoteOutcome != "unknown" || d.Attempts[3].ResultSHA256 != "original" || other.Status != "open" {
		t.Fatal(d.Attempts)
	}
}

func TestCompletionBDD_RefutedRootKeepsAnswersAndOtherStudiesAvailable(t *testing.T) {
	d := emptyData()
	d.Studies = append(d.Studies, Study{ID: "closed", Goal: "root"}, Study{ID: "other", Goal: "other-root"})
	root := newEntity("root", "Root", "goal", "closed", "False", "Prop")
	root.Status = "refuted"
	d.Entities = append(d.Entities, root, newEntity("other-root", "Other", "goal", "other", "True", "Prop"))
	d.Tasks = append(d.Tasks, Task{ID: "question", Kind: "answer"})
	d.Attempts = append(d.Attempts,
		Attempt{ID: "answer", Target: "root", TaskID: "question", Status: "queued"},
		Attempt{ID: "proof", Target: "root", Status: "queued"},
		Attempt{ID: "other", Target: "other-root", Status: "queued"},
	)
	stopClosedStudies(&d)
	if d.Attempts[0].Status != "queued" || d.Attempts[1].Status != "cancelled" || d.Attempts[2].Status != "queued" {
		t.Fatal(d.Attempts)
	}
	finished := *d.Attempts[1].FinishedAt
	stopClosedStudies(&d)
	if !finished.Equal(*d.Attempts[1].FinishedAt) {
		t.Fatal("repeated stop changed the terminal record")
	}
}
