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

func TestCompletionBDD_InterruptedClosedTeamFinishesOnceWithoutReplayingUnknownAttempt(t *testing.T) {
	s := serviceFor(t, optionsFor(t, "http://127.0.0.1:1/v1"))
	s.mu.Lock()
	err := s.Store.Change(0, "", "", "Closed interrupted fixture", "", "test", func(d *Data) error {
		root := newEntity("root", "Root", "goal", "closed", "True", "Prop")
		root.Status = "accepted"
		d.Entities = append(d.Entities, root, newEntity("other-root", "Other", "goal", "other", "True", "Prop"))
		d.Studies = append(d.Studies, Study{ID: "closed", Goal: "root"}, Study{ID: "other", Goal: "other-root"})
		d.Teams = append(d.Teams, ResearchTeam{ID: "closed-team", Study: "closed", Goal: "root", Status: "interrupted"}, ResearchTeam{ID: "other-team", Study: "other", Goal: "other-root", Status: "interrupted"})
		d.Attempts = append(d.Attempts, Attempt{ID: "unknown", Target: "root", Status: "interrupted", RemoteOutcome: "unknown", InputSHA256: "original-input"})
		return nil
	})
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	s.advanceTeams()
	before := stateOf(t, s)
	if before.Teams[0].Status != "completed" || before.Teams[1].Status != "interrupted" {
		t.Fatal("closed or unrelated team misclassified", before.Teams)
	}
	for range 3 {
		s.advanceTeams()
	}
	after := stateOf(t, s)
	if after.Revision != before.Revision || len(after.Attempts) != 1 || after.Attempts[0].RemoteOutcome != "unknown" || after.Attempts[0].InputSHA256 != "original-input" {
		t.Fatal("terminal reconciliation repeated a step or lost the unknown outcome")
	}
}
