package researchweb

import "github.com/snow-ghost/research-team/internal/leancheck"

const PilotID = "injective-iterations@1"

const leftInverseClaim = "forall (a b : Type) (f : a -> b) (g : b -> a), Function.LeftInverse g f -> Function.Injective f"
const compositionClaim = "forall (a b c : Type) (f : a -> b) (g : b -> c), Function.Injective f -> Function.Injective g -> Function.Injective (fun x => g (f x))"
const iterationsClaim = "forall (a : Type) (f : a -> a), Function.Injective f -> forall n : Nat, Function.Injective (f^[n])"
const pilotTarget = "forall (a : Type) (f g : a -> a), Function.LeftInverse g f -> forall n : Nat, Function.Injective (f^[n])"

type PilotRequest struct {
	Route            string `json:"route,omitempty"`
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Recipe           string `json:"recipe"`
	Confirm          bool   `json:"confirm"`
}

func pilotGoal(name, statement string) leancheck.Goal {
	return leancheck.Goal{Source: "import Mathlib\nnamespace ResearchIterations\ndef " + name + " : Prop := " + statement + "\nend ResearchIterations\n", Declaration: "ResearchIterations." + name, Candidate: "ResearchIterations." + name + "_candidate"}
}

// The recipe supplies statements, not accepted lemmas or pre-authorized model calls.
func (s *Service) CreatePilot(r PilotRequest) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || r.Recipe != PilotID || (r.Route != "" && r.Route != "decomposed" && r.Route != "direct") {
		return RuleError("Подтвердите известную учебную постановку и ее разбиение.")
	}
	return s.Store.Change(r.ExpectedRevision, r.RequestID, hash(r), "Создано учебное исследование итераций", "", "operator", func(d *Data) error {
		studyID, rootID := identifier("study"), identifier("H")
		title := "Инъективность итераций при наличии левого обратного"
		if r.Route == "direct" {
			title += ": прямая цель"
		}
		d.Studies = append(d.Studies, Study{ID: studyID, Title: title, Goal: rootID, Category: "Учебное исследование", Formula: pilotTarget})
		root := newEntity(rootID, "Инъективность каждой итерации", "goal", studyID, pilotTarget, "Отображения f и g действуют на одном типе; g является левым обратным f.")
		goal := pilotGoal("Target", pilotTarget)
		root.FormalGoal = &goal
		d.Entities = append(d.Entities, root)
		ids := []string{}
		claims := []struct{ name, title, statement string }{
			{"LeftInverseClaim", "Левое обратное дает инъективность", leftInverseClaim},
			{"CompositionClaim", "Композиция инъективных отображений", compositionClaim},
			{"IterationClaim", "Инъективность итераций через композицию", "(" + compositionClaim + ") -> (" + iterationsClaim + ")"},
			{"Coverage", "Переход от лемм к исходной цели", "(" + leftInverseClaim + ") -> ((" + compositionClaim + ") -> (" + iterationsClaim + ")) -> (" + compositionClaim + ") -> (" + pilotTarget + ")"},
		}
		for i, c := range claims {
			if r.Route == "direct" {
				break
			}
			id := identifier("L")
			e := newEntity(id, c.title, "lemma", studyID, c.statement, "Точные кванторы и условия заданы в закрепленной цели Lean.")
			g := pilotGoal(c.name, c.statement)
			e.FormalGoal = &g
			if i == 2 {
				e.Dependencies = []string{ids[1]}
			}
			if i == 3 {
				e.Kind = "obligation"
				e.Dependencies = append([]string{}, ids...)
			}
			d.Entities = append(d.Entities, e)
			for _, dep := range e.Dependencies {
				d.WorkLinks = append(d.WorkLinks, [2]string{id, dep})
			}
			ids = append(ids, id)
		}
		if r.Route != "direct" {
			d.entity(rootID).Dependencies = []string{ids[3]}
			d.WorkLinks = append(d.WorkLinks, [2]string{rootID, ids[3]})
		}
		d.addTask(rootID, "Прямая проверка исходной цели", "proof", "Проверь прямое доказательство средствами Mathlib как альтернативу разбиению. Сравни используемые леммы и обязательства; результаты ветвей не принимаются автоматически.")
		d.addTask(rootID, "Проверить замену левого обратного правым", "counterexample", "Проверь, можно ли заменить левое обратное правым без изменения вывода об инъективности. На том же типе Nat рассмотри f(n) = n / 2 и g(n) = 2 * n: f(g(n)) = n, но f(0) = f(1). Это проверка границы метода, а не контрпример к закрепленной цели.")
		return nil
	})
}
