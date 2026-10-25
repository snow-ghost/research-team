package researchweb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/snow-ghost/research-team/internal/execution"
)

// Only terminal preparation failures may lack a sealed input. Evidence-bearing
// proof, counterexample and review attempts still require the complete contract.
type IncompleteAttempt struct {
	Attempt             Attempt           `json:"attempt"`
	Task                Task              `json:"task"`
	SourceProfileSHA256 string            `json:"source_profile_sha256"`
	Result              *execution.Result `json:"result,omitempty"`
	Journal             []byte            `json:"journal,omitempty"`
	JournalSHA256       string            `json:"journal_sha256,omitempty"`
	Missing             []string          `json:"missing"`
	Reason              string            `json:"reason"`
}

func (s *Service) incompleteAttempt(v View, a Attempt, required map[string]bool) (IncompleteAttempt, error) {
	if a.ProfileConfiguration == nil {
		return IncompleteAttempt{}, RuleError("Неполная история требует закрепленного исходного профиля попытки.")
	}
	p := IncompleteAttempt{Attempt: a, SourceProfileSHA256: hash(attemptProfile(s, a)), Missing: []string{"scoped_input"}, Reason: "preparation_stopped_before_input_sealed"}
	p.Attempt.ProfileConfiguration = nil
	if required[a.ID] || a.ImportedPackageSHA256 != "" || a.RemoteOutcome != "not_started" || a.InputSHA256 != "" || !slices.Contains([]string{"failed", "cancelled"}, a.Status) || v.task(a.TaskID) == nil {
		return p, RuleError("Отсутствующий вход допустим только для завершенного отказа подготовки, не являющегося основанием результата.")
	}
	p.Task = *v.task(a.TaskID)
	if a.ResultSHA256 == "" {
		p.Missing = append(p.Missing, "result")
	} else {
		r, err := s.readResult(a)
		if err != nil || !s.safeResultContent(a, r) {
			return p, RuleError("Поврежден результат отказа подготовки.")
		}
		p.Result = &r
	}
	body, err := readBoundedFile(filepath.Join(s.Options.Config.DataDir, "attempts", a.ID, "events.jsonl"), 4<<20)
	if os.IsNotExist(err) {
		p.Missing = append(p.Missing, "journal")
	} else if err != nil {
		return p, err
	} else {
		p.Journal, p.JournalSHA256 = body, bytesDigest(body)
	}
	return p, nil
}

func validateIncomplete(p ResultPackage, seen map[string]bool) error {
	tasks := map[string]bool{}
	for _, x := range p.Attempts {
		if tasks[x.Attempt.TaskID] {
			return RuleError("Повторный идентификатор задания в пакете.")
		}
		tasks[x.Attempt.TaskID] = true
	}
	if len(p.IncompleteAttempts) > 64 || p.Version == 1 && len(p.IncompleteAttempts) != 0 {
		return ErrLimit
	}
	for _, x := range p.IncompleteAttempts {
		a := x.Attempt
		if tasks[a.TaskID] {
			return RuleError("Повторный идентификатор задания неполной истории.")
		}
		tasks[a.TaskID] = true
		if seen[a.ID] || !requestPattern.MatchString(a.ID) || !requestPattern.MatchString(a.TaskID) || a.Target != p.Target.ID || a.RemoteOutcome != "not_started" || !slices.Contains([]string{"failed", "cancelled"}, a.Status) || a.InputSHA256 != "" || a.ProfileConfiguration != nil || a.ImportedPackageSHA256 != "" || x.Task.ID != a.TaskID || x.Task.Attempt != a.ID || x.Task.Target != a.Target || x.Reason != "preparation_stopped_before_input_sealed" || len(x.SourceProfileSHA256) != 64 || !slices.Contains(x.Missing, "scoped_input") {
			return RuleError("Нарушено описание неполной истории.")
		}
		missing := map[string]bool{}
		for _, name := range x.Missing {
			if missing[name] || !slices.Contains([]string{"scoped_input", "result", "journal"}, name) {
				return RuleError("Неизвестный или повторный признак отсутствия.")
			}
			missing[name] = true
		}
		if missing["result"] != (x.Result == nil) || x.Result == nil && a.ResultSHA256 != "" || x.Result != nil && (a.ResultSHA256 != hash(*x.Result) || x.Result.AttemptID != a.ID || x.Result.TaskID != a.TaskID || x.Result.RemoteOutcome != "not_started") {
			return RuleError("Подменен результат неполной попытки.")
		}
		if missing["journal"] && (len(x.Journal) != 0 || x.JournalSHA256 != "") || !missing["journal"] && x.JournalSHA256 != bytesDigest(x.Journal) {
			return RuleError("Подменен журнал неполной попытки.")
		}
		if a.ID == p.Result.ReviewAttempt || a.ID == p.Result.CounterAttempt || a.ID == p.Verification.Attempt {
			return RuleError("Обязательное доказательство не может иметь неполные материалы.")
		}
		seen[a.ID] = true
	}
	return nil
}

func incompleteFiles(x IncompleteAttempt) map[string][]byte {
	files := map[string][]byte{}
	if x.Result != nil {
		files["result.json"], _ = json.Marshal(x.Result)
	}
	if !slices.Contains(x.Missing, "journal") {
		files["events.jsonl"] = x.Journal
	}
	return files
}
