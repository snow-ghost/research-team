package proofreview

import (
	"errors"
	"fmt"

	"github.com/snow-ghost/research-team/internal/leancheck"
)

type ExpectedIssue struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Description string `json:"description"`
}
type CalibrationCase struct {
	Exposition  string          `json:"exposition,omitempty"`
	ID          string          `json:"id"`
	Statement   string          `json:"statement"`
	Assumptions string          `json:"assumptions"`
	Kind        string          `json:"kind"`
	Goal        leancheck.Goal  `json:"goal"`
	Source      string          `json:"source"`
	Expected    []ExpectedIssue `json:"expected"`
}

// Exposition defects are independent of the correctness of the supplied Lean term.
func ExpandedCalibrationCases() []CalibrationCase {
	cases := CalibrationCases()
	add := func(id, body, statement, exposition, proof string, issues []ExpectedIssue) {
		name := fmt.Sprintf("CalibrationTarget%03d", len(cases)+1)
		g := leancheck.Goal{Source: "import Mathlib\ndef " + name + " : Prop := " + body + "\n", Declaration: name, Candidate: name + "Proof"}
		cases = append(cases, CalibrationCase{ID: id, Kind: "proof", Statement: statement, Assumptions: "Только явно перечисленные предпосылки формальной цели.", Exposition: exposition, Goal: g, Source: "import Goal\ntheorem " + g.Candidate + " : " + g.Declaration + " := by\n" + proof + "\n", Expected: issues})
	}
	add("exposition_correct", "forall n : Nat, n + 0 = n", "Для любого натурального n верно n+0=n.", "Сложение натурального числа с нулем справа по определению дает исходное число; равенство рефлексивно.", "  intro n\n  rfl", []ExpectedIssue{})
	add("exposition_circle", "forall n : Nat, n < n + 1", "Для любого натурального n верно n<n+1.", "Зафиксируем произвольное n. Предположим, что n<n+1. Тогда n<n+1. Поскольку n произвольно, без дополнительных предпосылок получено утверждение для всех n.", "  intro n\n  exact Nat.lt_succ_self n", []ExpectedIssue{{ID: "circular_argument", Category: "logic", Description: "Из допущения требуемого утверждения получено только P -> P, а не P; гипотеза не устранена. Корректный Lean-файл не исправляет этот круг в изложении."}})
	add("exposition_lemma_misuse", "forall a b : Nat, a + 1 = b + 1 -> a = b", "Если a+1=b+1 для натуральных a,b, то a=b.", "Из a+1=b+1 получаем рефлексивное a=a. Применяя правило рефлексивности равенства к a=a, заключаем a=b. Рефлексивность позволяет заменить правую часть равенства произвольным b.", "  intro a b h\n  omega", []ExpectedIssue{{ID: "invalid_lemma_application", Category: "lemma", Description: "Рефлексивность дает только a=a и не позволяет заключить a=b с произвольным b. Само утверждение истинно, но указанное применение правила неверно."}})
	add("exposition_missing_case", "forall n : Nat, n <= n + 1", "Для любого натурального n верно n<=n+1.", "Рассмотрим только четные n=2*k. Из неотрицательности единицы следует 2*k<=2*k+1. Этим доказано утверждение для всех натуральных n; нечетные числа не рассматриваются и перехода к ним нет.", "  intro n\n  omega", []ExpectedIssue{{ID: "missing_case", Category: "coverage", Description: "Изложение проверяет только четные числа, но заявляет общий вывод без случая нечетных чисел и без основания его исключения."}})
	add("exposition_hidden_premise", "forall x : Real, 0 <= x * x", "Квадрат любого действительного x неотрицателен.", "Зафиксируем действительное x и дополнительно предположим x>0. Произведение двух положительных чисел положительно, следовательно x*x>=0. Дополнительная гипотеза x>0 не устраняется; результат объявляется доказанным для всех действительных x.", "  intro x\n  exact mul_self_nonneg x", []ExpectedIssue{{ID: "unremoved_positive_premise", Category: "premise", Description: "Изложение использует x>0, которого нет в исходной постановке, и не разбирает x<=0. Это дефект изложения, не опровержение утверждения или Lean-файла."}})
	add("exposition_composition_correct", "forall p q r : Prop, (p -> q) -> (q -> r) -> p -> r", "Из P->Q, Q->R и P следует R.", "Из первой импликации и P по правилу отделения получаем Q. Из второй импликации и Q тем же правилом получаем R. Все три исходные предпосылки перечислены в постановке.", "  intro p q r hpq hqr hp\n  exact hqr (hpq hp)", []ExpectedIssue{})
	return cases
}

// Expected issues are reference annotations and must never be sent to a reviewer.
func CalibrationCases() []CalibrationCase {
	goal := func(id, body string) leancheck.Goal {
		return leancheck.Goal{Source: "import Mathlib\ndef " + id + " : Prop := " + body + "\n", Declaration: id, Candidate: id + "Proof"}
	}
	proof := func(g leancheck.Goal, body string) string {
		return "import Goal\ntheorem " + g.Candidate + " : " + g.Declaration + " := by\n" + body + "\n"
	}
	identity := goal("CalibrationTarget001", "forall p : Prop, p -> p")
	falseGoal := goal("CalibrationTarget002", "forall n : Nat, n + 1 = n")
	bound := goal("CalibrationTarget003", "forall n : Nat, n < n + 1")
	left := goal("CalibrationTarget004", "forall (f g : Nat -> Nat), (forall x, g (f x) = x) -> forall x y, f x = f y -> x = y")
	conjunction := goal("CalibrationTarget005", "forall p q : Prop, p ∧ q -> p")
	finite := goal("CalibrationTarget006", "forall n : Nat, n ≤ 10 -> n < 11")
	return []CalibrationCase{
		{ID: "identity", Statement: "Для любого высказывания P из P следует P.", Assumptions: "P : Prop.", Kind: "proof", Goal: identity, Source: proof(identity, "  intro p h\n  exact h"), Expected: []ExpectedIssue{}},
		{ID: "refutation", Statement: "Для каждого натурального n верно n + 1 = n. Проверить утверждение или его отрицание.", Assumptions: "n : Nat; опровержение является допустимым исходом.", Kind: "refutation", Goal: falseGoal, Source: "import Goal\ntheorem ResearchRefutation.candidate : ResearchRefutation.Statement := by\n  intro h\n  have bad := h 0\n  omega\n", Expected: []ExpectedIssue{}},
		{ID: "changed_bound", Statement: "Для любого натурального n верно n + 1 > 2 * n.", Assumptions: "n : Nat, без дополнительных ограничений.", Kind: "proof", Goal: bound, Source: proof(bound, "  intro n\n  exact Nat.lt_succ_self n"), Expected: []ExpectedIssue{{ID: "weakened_conclusion", Category: "statement", Description: "Формальная цель доказывает n < n+1 вместо 2*n < n+1. Исходное утверждение ложно при n=2."}}},
		{ID: "extra_premise", Statement: "Любая функция f : Nat -> Nat инъективна.", Assumptions: "Для f не предполагается существование обратной функции.", Kind: "proof", Goal: left, Source: proof(left, "  intro f g hi x y heq\n  calc\n    x = g (f x) := (hi x).symm\n    _ = g (f y) := congrArg g heq\n    _ = y := hi y"), Expected: []ExpectedIssue{{ID: "added_inverse", Category: "premise", Description: "Формальная цель требует левую обратную функцию, отсутствующую в постановке. Постоянная функция дает контрпример исходному утверждению."}}},
		{ID: "changed_connective", Statement: "Для любых высказываний P и Q из P или Q следует P.", Assumptions: "P,Q : Prop; дополнительных предпосылок нет.", Kind: "proof", Goal: conjunction, Source: proof(conjunction, "  intro p q h\n  exact h.1"), Expected: []ExpectedIssue{{ID: "and_instead_of_or", Category: "statement", Description: "В формальной цели использовано P ∧ Q вместо P ∨ Q. Исходное утверждение ложно для P=False,Q=True."}}},
		{ID: "bounded_domain", Statement: "Каждое натуральное число меньше 11.", Assumptions: "Все натуральные числа, без верхнего ограничения.", Kind: "proof", Goal: finite, Source: proof(finite, "  intro n hn\n  omega"), Expected: []ExpectedIssue{{ID: "restricted_domain", Category: "premise", Description: "В формальной цели добавлено n≤10. При n=11 исходная постановка ложна."}}},
	}
}
func CalibrationClaim(c CalibrationCase) leancheck.Goal {
	if c.Kind != "refutation" {
		return c.Goal
	}
	return leancheck.Goal{Source: c.Goal.Source + "\nnamespace ResearchRefutation\ndef Statement : Prop := Not (" + c.Goal.Declaration + ")\nend ResearchRefutation\n", Declaration: "ResearchRefutation.Statement", Candidate: "ResearchRefutation.candidate"}
}

type FindingAnnotation struct {
	Index int    `json:"index"`
	Issue string `json:"issue,omitempty"`
}
type ReviewAnnotation struct {
	Case         string              `json:"case"`
	ReportSHA256 string              `json:"report_sha256"`
	Annotator    string              `json:"annotator"`
	Findings     []FindingAnnotation `json:"findings"`
}
type CalibrationScore struct {
	Expected    int      `json:"expected"`
	Detected    int      `json:"detected"`
	Missed      int      `json:"missed"`
	FalseAlarms int      `json:"false_alarms"`
	Duplicates  int      `json:"duplicates"`
	Editorial   int      `json:"editorial"`
	Precision   *float64 `json:"precision,omitempty"`
	Recall      *float64 `json:"recall,omitempty"`
}

func ScoreCalibration(c CalibrationCase, r Report, a ReviewAnnotation) (CalibrationScore, error) {
	s := CalibrationScore{Expected: len(c.Expected)}
	if Validate(r) != nil || a.Case != c.ID || a.ReportSHA256 != leancheck.Digest(r) || a.Annotator == "" || len(a.Findings) != len(r.Findings) {
		return s, errors.New("complete source-bound annotations are required")
	}
	expected := map[string]bool{}
	for _, issue := range c.Expected {
		if issue.ID == "" || expected[issue.ID] {
			return s, errors.New("invalid reference issue")
		}
		expected[issue.ID] = true
	}
	seen, detected := map[int]bool{}, map[string]bool{}
	for _, annotation := range a.Findings {
		if annotation.Index < 0 || annotation.Index >= len(r.Findings) || seen[annotation.Index] {
			return s, errors.New("finding index missing or duplicated")
		}
		seen[annotation.Index] = true
		if annotation.Issue != "" && !expected[annotation.Issue] {
			return s, fmt.Errorf("unknown reference issue: %s", annotation.Issue)
		}
		if r.Findings[annotation.Index].Severity == "editorial" {
			s.Editorial++
			continue
		}
		if annotation.Issue == "" {
			s.FalseAlarms++
			continue
		}
		if detected[annotation.Issue] {
			s.Duplicates++
		}
		detected[annotation.Issue] = true
	}
	s.Detected = len(detected)
	s.Missed = s.Expected - s.Detected
	if n := s.Detected + s.FalseAlarms; n > 0 {
		value := float64(s.Detected) / float64(n)
		s.Precision = &value
	}
	if s.Expected > 0 {
		value := float64(s.Detected) / float64(s.Expected)
		s.Recall = &value
	}
	return s, nil
}
