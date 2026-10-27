package leancheck

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const PreparationVersion = "goal-preparation@1"

type PreparedDefinition struct {
	Goal   Goal         `json:"goal"`
	Module string       `json:"module"`
	Source string       `json:"source"`
	Report ModuleReport `json:"report"`
}
type GoalPreparation struct {
	Version           string               `json:"version"`
	Status            string               `json:"status"`
	GoalsSHA256       string               `json:"goals_sha256"`
	EnvironmentSHA256 string               `json:"environment_sha256"`
	Definitions       []PreparedDefinition `json:"definitions"`
	Joint             *Report              `json:"joint,omitempty"`
	Diagnostics       string               `json:"diagnostics,omitempty"`
}
type GoalPreparer interface {
	PrepareGoals(context.Context, []Goal, string) (GoalPreparation, error)
}

// Only the identity P -> P is proved here. Preparing definitions never proves P.
func definitionProbe(g Goal) (Goal, string) {
	ns := "ResearchPreparation_" + Digest(g)[:32]
	target := "_root_." + g.Declaration
	probe := Goal{Source: g.Source + "\nnamespace " + ns + "\ndef Statement : Prop := (" + target + ") -> (" + target + ")\nend " + ns + "\n", Declaration: ns + ".Statement", Candidate: ns + ".witness"}
	return probe, "import Goal\ntheorem " + probe.Candidate + " : " + probe.Declaration + " := by\n  intro h\n  exact h\n"
}

func jointPreparation(definitions []PreparedDefinition) (Goal, string) {
	var imports strings.Builder
	for _, d := range definitions {
		fmt.Fprintln(&imports, "import "+d.Module)
	}
	imports.WriteString("\ndef ResearchJointPreparation : Prop := True\n")
	return Goal{Source: imports.String(), Declaration: "ResearchJointPreparation", Candidate: "researchJointPreparationWitness"}, "import Goal\ntheorem researchJointPreparationWitness : ResearchJointPreparation := by trivial\n"
}

func (c DockerChecker) PrepareGoals(ctx context.Context, goals []Goal, dir string) (GoalPreparation, error) {
	r := GoalPreparation{Version: PreparationVersion, Status: "failed", GoalsSHA256: Digest(goals), EnvironmentSHA256: Digest(c.Config), Definitions: []PreparedDefinition{}}
	if len(goals) < 1 || len(goals) > 8 || !filepath.IsAbs(dir) {
		return r, errors.New("preparation requires 1..8 goals and an absolute private directory")
	}
	if err := c.Config.Validate(); err != nil {
		return r, err
	}
	checked := filepath.Join(dir, "checked")
	if err := os.Mkdir(checked, 0700); err != nil {
		return r, err
	}
	for i, g := range goals {
		if err := g.Validate(); err != nil {
			return r, err
		}
		probe, source := definitionProbe(g)
		module := "ResearchDefinitions_" + Digest(g)[:32]
		work := filepath.Join(dir, fmt.Sprintf("definition-%d", i))
		if err := os.Mkdir(work, 0700); err != nil {
			return r, err
		}
		report, err := c.BuildModule(ctx, probe, source, module, work)
		combined, combineErr := ModuleSource(probe, source)
		if combineErr != nil {
			return r, combineErr
		}
		r.Definitions = append(r.Definitions, PreparedDefinition{Goal: g, Module: module, Source: combined, Report: report})
		if err != nil || report.Status != "ready" {
			r.Diagnostics = report.Diagnostics
			return r, err
		}
		if report.SourceSHA256 != Digest(combined) || report.EnvironmentSHA256 != r.EnvironmentSHA256 || report.AuditSHA256 != AuditDigest() {
			return r, errors.New("definition report binding mismatch")
		}
		if err := VerifyModuleArtifacts(filepath.Join(work, "checked"), report); err != nil {
			return r, err
		}
		for name := range report.Artifacts {
			body, err := os.ReadFile(filepath.Join(work, "checked", name))
			if err != nil {
				return r, err
			}
			f, err := os.OpenFile(filepath.Join(checked, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return r, err
			}
			_, writeErr := f.Write(body)
			closeErr := f.Close()
			if writeErr != nil {
				return r, writeErr
			}
			if closeErr != nil {
				return r, closeErr
			}
		}
	}
	joint, jointSource := jointPreparation(r.Definitions)
	env := c.Config
	env.Libraries = append(append([]string{}, env.Libraries...), checked)
	work := filepath.Join(dir, "joint")
	if err := os.Mkdir(work, 0700); err != nil {
		return r, err
	}
	report, err := (DockerChecker{Config: env}).Check(ctx, joint, jointSource, work)
	r.Joint = &report
	if err != nil || report.Status != "verified" {
		r.Diagnostics = report.Diagnostics
		return r, err
	}
	if report.GoalSHA256 != Digest(joint) || report.SourceSHA256 != Digest(jointSource) || report.AuditSHA256 != AuditDigest() || report.EnvironmentSHA256 != Digest(env) {
		return r, errors.New("joint preparation binding mismatch")
	}
	r.Status = "ready"
	return r, nil
}

func VerifyPreparedDefinitions(dir string, r GoalPreparation, goals []Goal) error {
	if r.Version != PreparationVersion || r.Status != "ready" || r.GoalsSHA256 != Digest(goals) || len(r.Definitions) != len(goals) || r.Joint == nil || r.Joint.Status != "verified" || r.Joint.AuditSHA256 != AuditDigest() {
		return errors.New("preparation does not match goals")
	}
	joint, source := jointPreparation(r.Definitions)
	if r.Joint.GoalSHA256 != Digest(joint) || r.Joint.SourceSHA256 != Digest(source) {
		return errors.New("joint preparation does not match definitions")
	}
	for i, d := range r.Definitions {
		probe, source := definitionProbe(goals[i])
		combined, err := ModuleSource(probe, source)
		if err != nil || Digest(d.Goal) != Digest(goals[i]) || d.Module != "ResearchDefinitions_"+Digest(goals[i])[:32] || d.Report.Module != d.Module || d.Source != combined || d.Report.SourceSHA256 != Digest(combined) || d.Report.EnvironmentSHA256 != r.EnvironmentSHA256 || d.Report.AuditSHA256 != AuditDigest() {
			return errors.New("prepared definition binding mismatch")
		}
		if err := VerifyModuleArtifacts(filepath.Join(dir, "checked"), d.Report); err != nil {
			return err
		}
	}
	return nil
}
