package researchweb

import (
	"context"
	"os/exec"
	"regexp"
	"time"

	"github.com/snow-ghost/research-team/internal/researchcompare"
)

var evaluationUnit = regexp.MustCompile(`^research-team-evaluation[a-zA-Z0-9_-]{1,80}\.service$`)

type EvaluationCommand struct {
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	Kind             string `json:"kind"`
	Note             string `json:"note"`
	Confirm          bool   `json:"confirm"`
}

func (s *Service) ControlEvaluation(id string, r EvaluationCommand) error {
	if !r.Confirm || r.ExpectedRevision < 1 || !requestPattern.MatchString(r.RequestID) || !textOK(r.Note, 4000) || r.Kind != "stop" && r.Kind != "deliver" {
		return RuleError("Подтвердите остановку или запись завершенного отчета и основание.")
	}
	var watch *EvaluationWatch
	for _, w := range s.Options.Config.Evaluations {
		if w.ID == id {
			copy := w
			watch = &copy
			break
		}
	}
	if watch == nil {
		return RuleError("Опыт не найден.")
	}
	if r.Kind == "deliver" {
		report, err := researchcompare.ReadReport(watch.File)
		if err != nil || report.Simulation || report.ConfigurationSHA256 != watch.ConfigurationSHA256 {
			return RuleError("Закрепленный отчет недоступен.")
		}
		return s.ImportComparison(ComparisonRequest{ExpectedRevision: r.ExpectedRevision, RequestID: r.RequestID, Report: report, Confirm: true})
	}
	if !evaluationUnit.MatchString(watch.ProcessUnit) {
		return RuleError("Остановка службы не настроена оператором.")
	}
	// Persist intent before the side effect. Repeating a recorded command never
	// repeats systemctl; after a crash its outcome must be observed separately.
	fresh := false
	if err := s.Store.Change(r.ExpectedRevision, r.RequestID, hash(struct {
		ID      string
		Command EvaluationCommand
	}{id, r}), "Запрошена остановка отдельного сравнения: "+r.Note, id, "operator", func(d *Data) error {
		if d.Maintenance != nil {
			return RuleError("Сначала завершите обслуживание.")
		}
		fresh = true
		return nil
	}); err != nil {
		return err
	}
	if !fresh {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stop := s.Options.StopEvaluation
	if stop == nil {
		stop = func(ctx context.Context, unit string) error {
			return exec.CommandContext(ctx, "/usr/bin/systemctl", "--user", "stop", unit).Run()
		}
	}
	err := stop(ctx, watch.ProcessUnit)
	label := "Местная служба сравнения остановлена; отмена удаленного запроса не подтверждена"
	if err != nil {
		label = "Исход остановки службы не подтвержден; повтор автоматически не назначается"
	}
	saveErr := s.Store.Change(0, "", "", label, id, "coordinator", func(*Data) error { return nil })
	if err != nil {
		return RuleError("Остановка не подтверждена. Проверьте состояние службы; новые модельные запросы не назначались.")
	}
	return saveErr
}
