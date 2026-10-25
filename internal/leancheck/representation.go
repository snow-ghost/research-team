package leancheck

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func RepresentationDigest() string {
	return Digest([]string{"representation-transfer@1;operator-declared-goals;same-source;iff-bridge;four-bounded-checks;no-model;no-acceptance", reuseTemplate, AuditDigest(), DependencyDigest()})
}

type RepresentationReport struct {
	ProcedureSHA256    string    `json:"procedure_sha256"`
	OriginalGoalSHA256 string    `json:"original_goal_sha256"`
	BridgeAuthor       string    `json:"bridge_author"`
	Depth              int       `json:"depth"`
	TimeoutSeconds     int       `json:"timeout_seconds"`
	Original           ReuseArm  `json:"original"`
	Direct             ReuseArm  `json:"direct"`
	Bridge             ReuseArm  `json:"bridge"`
	Transferred        *ReuseArm `json:"transferred,omitempty"`
	ModelCalls         int       `json:"model_calls"`
	Accepted           bool      `json:"accepted"`
}

// The operator supplies declarations in one immutable source. The checker
// proves the bridge is an Iff and audits the final candidate against the
// original declaration; a successful search alone cannot change the statement.
func CompareRepresentation(ctx context.Context, checker Checker, original, direct, bridge Goal, bridgeSource, author, candidate string, depth, seconds int, dir string) (RepresentationReport, error) {
	r := RepresentationReport{ProcedureSHA256: RepresentationDigest(), OriginalGoalSHA256: Digest(original), BridgeAuthor: author, Depth: depth, TimeoutSeconds: seconds}
	if checker == nil || original.Validate() != nil || direct.Validate() != nil || bridge.Validate() != nil || original.Source != direct.Source || original.Source != bridge.Source || original.Declaration == direct.Declaration || bridge.Declaration == original.Declaration || bridge.Declaration == direct.Declaration || original.Candidate == direct.Candidate || bridge.Candidate == original.Candidate || bridge.Candidate == direct.Candidate || !declaration.MatchString(candidate) || strings.TrimSpace(author) == "" || len(author) > 300 || depth < 1 || depth > 6 || seconds < 1 || seconds > 120 {
		return r, errors.New("need distinct declarations, immutable shared source, explicit bridge author and bounds")
	}
	bridgeBody, ok := strings.CutPrefix(bridgeSource, "import Goal\n")
	if !ok || len(bridgeSource) > 64000 {
		return r, errors.New("bridge must begin with import Goal")
	}
	check := func(name string, g Goal, source string) (ReuseArm, error) {
		work := filepath.Join(dir, name)
		if err := os.Mkdir(work, 0700); err != nil {
			return ReuseArm{}, err
		}
		limited, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
		defer cancel()
		start := time.Now()
		report, err := checker.Check(limited, g, source, work)
		a := ReuseArm{Status: "not_proved", Source: source, Goal: g, Report: report, Seconds: time.Since(start).Seconds()}
		if report.Status == "verified" {
			if report.AuditSHA256 != AuditDigest() || report.GoalSHA256 != Digest(g) || report.SourceSHA256 != Digest(source) {
				return a, errors.New("representation report binding differs")
			}
			a.Status = "verified"
		} else if err != nil && report.Phase != "compile" {
			a.Status = "failed"
		}
		return a, nil
	}
	var err error
	for _, item := range []struct {
		name string
		goal Goal
		dest *ReuseArm
	}{{"original", original, &r.Original}, {"direct", direct, &r.Direct}} {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		source := fmt.Sprintf(reuseTemplate, item.goal.Candidate, item.goal.Declaration, item.goal.Declaration, depth+1, "*, "+candidate)
		*item.dest, err = check(item.name, item.goal, source)
		if err != nil {
			return r, err
		}
	}
	// Requiring this exact type checks both directions, independently of the
	// operator's chosen name or natural-language description of the bridge.
	bridgeCheck := bridge
	bridgeCheck.Source += fmt.Sprintf("\nexample : %s = (%s ↔ %s) := rfl\n", bridge.Declaration, direct.Declaration, original.Declaration)
	r.Bridge, err = check("bridge", bridgeCheck, bridgeSource)
	if err != nil {
		return r, err
	}
	if r.Direct.Status != "verified" || r.Bridge.Status != "verified" {
		return r, nil
	}
	body, _ := strings.CutPrefix(r.Direct.Source, "import Goal\n")
	source := "import Goal\n" + body + "\n" + bridgeBody + fmt.Sprintf("\ntheorem %s : %s := (%s).mp %s\n", original.Candidate, original.Declaration, bridge.Candidate, direct.Candidate)
	a, err := check("transferred", original, source)
	r.Transferred = &a
	return r, err
}
