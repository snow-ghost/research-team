//go:build linux

package researchweb

import (
	"context"
	"errors"
	"testing"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

const submittedProof = "import Goal\ntheorem Candidate : Statement := by trivial\n"

func submittedService(t *testing.T, backend, outcome string) (*Service, string) {
	t.Helper()
	o := optionsFor(t, "http://127.0.0.1:1/v1")
	o.Checker = checkerFunc(func(_ context.Context, g leancheck.Goal, source, _ string) (leancheck.Report, error) {
		r := leancheck.Report{AuditSHA256: leancheck.AuditDigest(), Status: outcome, GoalSHA256: leancheck.Digest(g), SourceSHA256: leancheck.Digest(source)}
		if outcome == "wrong_hash" {
			r.Status, r.SourceSHA256 = "verified", "different source"
		}
		return r, nil
	})
	if backend == "postgres" {
		dsn := postgresDSN(t)
		o.Config.Database = DatabaseConfig{Driver: "postgres", DSNEnv: "TEST_DB"}
		lookup := o.Lookup
		o.Lookup = func(key string) (string, bool) {
			if key == "TEST_DB" {
				return dsn, true
			}
			return lookup(key)
		}
	}
	s := serviceFor(t, o)
	v := studyFor(t, s)
	goal := v.Studies[0].Goal
	if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: leancheck.Goal{Source: "def Statement : Prop := True", Declaration: "Statement", Candidate: "Candidate"}}); err != nil {
		t.Fatal(err)
	}
	return s, goal
}

func submitFor(t *testing.T, s *Service, goal, author string) string {
	t.Helper()
	r := ProofSourceRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Source: submittedProof, Author: author, Basis: "Prepared separately from executor attempts."}
	if err := s.SubmitProofSource(goal, r); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitProofSource(goal, r); err != nil {
		t.Fatal("idempotent retry", err)
	}
	v := stateOf(t, s)
	if len(v.Verifications) != 1 || len(v.Attempts) != 0 {
		t.Fatal("duplicate check or synthetic attempt")
	}
	return v.Verifications[0].ID
}

func TestSubmittedProofBDD_ProvenanceVerificationAndSeparateAcceptance(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			s, goal := submittedService(t, backend, "verified")
			id := submitFor(t, s, goal, "Codex")
			v := waitVerification(t, s, id)
			if v.entity(goal).Proof != "" || hasVerifiedProof(&v.Data, v.entity(goal)) {
				t.Fatal("verification implicitly attached proof")
			}
			r := AttachVerificationRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd")}
			if err := s.AttachVerification(id, r); err != nil {
				t.Fatal(err)
			}
			if err := s.AttachVerification(id, r); err != nil {
				t.Fatal("idempotent attachment", err)
			}
			v = stateOf(t, s)
			e := v.entity(goal)
			if e.Status != "in_review" || e.ProofAttempt != "" || e.ProofVerification != id || e.ProofAuthor != "Codex" || !hasVerifiedProof(&v.Data, e) {
				t.Fatalf("incorrect provenance or status: %+v", e)
			}
			if v.verification(id).SubmittedRevision != 2 || e.Revision != 3 {
				t.Fatal("lost original version")
			}
			modified := *e
			modified.Proof += "-- changed\n"
			if hasVerifiedProof(&v.Data, &modified) {
				t.Fatal("verified substituted source")
			}
			modified = *e
			modified.ProofAuthor = "Another author"
			if hasVerifiedProof(&v.Data, &modified) {
				t.Fatal("verified substituted author")
			}
			if backend == "postgres" {
				func() {
					s.Store.pg.mu.Lock()
					defer s.Store.pg.mu.Unlock()
					var emptyAttempt bool
					if err := s.Store.pg.conn.QueryRowContext(context.Background(), "SELECT attempt_id IS NULL FROM proof_verifications WHERE id=$1", id).Scan(&emptyAttempt); err != nil || !emptyAttempt {
						t.Fatal("submission requires synthetic attempt", err)
					}
					if _, err := s.Store.pg.conn.ExecContext(context.Background(), "UPDATE proof_verifications SET data=data-'origin' WHERE id=$1", id); err == nil {
						t.Fatal("missing provenance passed SQL constraint")
					}
					if _, err := s.Store.pg.conn.ExecContext(context.Background(), "UPDATE entities SET data=jsonb_set(data,'{proofVerification}','\"missing\"') WHERE id=$1", goal); err == nil {
						t.Fatal("missing verification passed foreign key")
					}
				}()
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = serviceFor(t, s.Options)
			v = stateOf(t, s)
			if !hasVerifiedProof(&v.Data, v.entity(goal)) || v.entity(goal).Status != "in_review" {
				t.Fatal("restart lost registered proof")
			}
			act(t, s, Action{Type: "REVIEW", Target: goal, Decision: "accept", Text: "Проверены постановка и формальное доказательство."})
			v = stateOf(t, s)
			if v.entity(goal).Status != "accepted" {
				t.Fatal("explicit acceptance failed")
			}
		})
	}
}

func TestSubmittedProofBDD_FailedStaleAndSelfAuthoredMaterialsAreBlocked(t *testing.T) {
	for _, outcome := range []string{"failed", "wrong_hash", "changed_goal", "changed_dependency", "operator"} {
		t.Run(outcome, func(t *testing.T) {
			checkerOutcome := outcome
			if outcome != "failed" && outcome != "wrong_hash" {
				checkerOutcome = "verified"
			}
			s, goal := submittedService(t, "sqlite", checkerOutcome)
			if outcome == "changed_dependency" {
				v := stateOf(t, s)
				v = act(t, s, Action{Type: "LEMMA", Study: v.Studies[0].ID, Title: "Lemma", Statement: "True", Assumptions: "None"})
				if err := s.Store.Change(v.Revision, "", "", "Fixture dependency", goal, "test", func(d *Data) error { d.entity(goal).Dependencies = []string{d.Entities[1].ID}; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			author := "Codex"
			if outcome == "operator" {
				author = " operator "
			}
			id := submitFor(t, s, goal, author)
			v := waitVerification(t, s, id)
			if outcome == "changed_goal" {
				changed := *v.entity(goal).FormalGoal
				changed.Source = "def Statement : Prop := False"
				if err := s.SetFormalGoal(goal, FormalGoalRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd"), Goal: changed}); err != nil {
					t.Fatal(err)
				}
			}
			if outcome == "changed_dependency" {
				if err := s.Store.Change(v.Revision, "", "", "Fixture change", goal, "test", func(d *Data) error { d.Entities[1].Revision++; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			v = stateOf(t, s)
			err := s.AttachVerification(id, AttachVerificationRequest{ExpectedRevision: v.Revision, RequestID: identifier("cmd")})
			if outcome == "operator" {
				if err != nil {
					t.Fatal(err)
				}
				v = stateOf(t, s)
				err = s.Act(Action{Type: "REVIEW", Target: goal, Decision: "accept", Text: "Проверено.", ExpectedRevision: v.Revision, RequestID: identifier("cmd")})
			}
			if err == nil {
				t.Fatal("unsafe material accepted")
			}
		})
	}
}

func TestSubmittedProofBDD_ValidationAndRevisionConflicts(t *testing.T) {
	s, goal := submittedService(t, "sqlite", "verified")
	r := ProofSourceRequest{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Source: submittedProof, Author: "Codex", Basis: "External preparation"}
	bad := r
	bad.Author = " "
	if err := s.SubmitProofSource(goal, bad); err == nil {
		t.Fatal("empty author accepted")
	}
	bad = r
	bad.Source = " "
	if err := s.SubmitProofSource(goal, bad); err == nil {
		t.Fatal("empty source accepted")
	}
	bad = r
	bad.ExpectedRevision--
	if err := s.SubmitProofSource(goal, bad); !errors.Is(err, ErrConflict) {
		t.Fatal("stale revision accepted", err)
	}
	if len(stateOf(t, s).Verifications) != 0 {
		t.Fatal("invalid requests wrote state")
	}
}
