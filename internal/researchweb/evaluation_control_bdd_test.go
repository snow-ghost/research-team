package researchweb

import (
	"context"
	"errors"
	"testing"
)

func TestEvaluationControlBDD_OnlyConfiguredUnitAndNoReplay(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "unknown"}[fail], func(t *testing.T) {
			calls := 0
			o := optionsFor(t, "http://127.0.0.1:1/v1")
			o.Config.Evaluations = []EvaluationWatch{{ID: "test-evaluation", ProcessUnit: "research-team-evaluation-fixture.service"}}
			o.StopEvaluation = func(_ context.Context, unit string) error {
				calls++
				if unit != "research-team-evaluation-fixture.service" {
					t.Fatal("wrong unit", unit)
				}
				if fail {
					return errors.New("transport unknown")
				}
				return nil
			}
			s := serviceFor(t, o)
			r := EvaluationCommand{ExpectedRevision: stateOf(t, s).Revision, RequestID: identifier("cmd"), Kind: "stop", Note: "Explicit fixture stop", Confirm: true}
			err := s.ControlEvaluation("test-evaluation", r)
			if (err != nil) != fail {
				t.Fatal(err)
			}
			if err := s.ControlEvaluation("test-evaluation", r); err != nil {
				t.Fatal("intent should not be repeated", err)
			}
			if calls != 1 || len(stateOf(t, s).Attempts) != 0 {
				t.Fatal("side effect repeated or model launched", calls)
			}
			r.ExpectedRevision = stateOf(t, s).Revision
			r.RequestID = identifier("cmd")
			r.Kind = "resume"
			if s.ControlEvaluation("test-evaluation", r) == nil {
				t.Fatal("external experiment resumed")
			}
			for _, unit := range []string{"research-team.service", "--now", "research-team-evaluation-x.service;id"} {
				s.Options.Config.Evaluations[0].ProcessUnit = unit
				r.Kind = "stop"
				if s.ControlEvaluation("test-evaluation", r) == nil {
					t.Fatal("unsafe unit accepted", unit)
				}
			}
		})
	}
}
