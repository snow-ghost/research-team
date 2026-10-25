package researchweb

import "github.com/snow-ghost/research-team/internal/execution"

func repairableSkillFailure(r execution.Result) bool {
	if r.Status != "failed" || r.PrecheckStatus != "candidate" || r.FailureCode != "skill_result_contract" || r.RemoteOutcome != "response_received" {
		return false
	}
	failed := false
	for _, check := range r.SkillChecks {
		if check.Check != "lean_source" && check.Check != "review_report" && check.Check != "counterexample_report" {
			return false
		}
		failed = failed || !check.Passed
	}
	return failed
}

func (s *Service) repairTeamFormat(d *Data, team *ResearchTeam, role string, a *Attempt) (bool, error) {
	if team.FormatRepairsUsed >= team.FormatRepairLimit || a.Status != "failed" || a.RemoteOutcome != "response_received" || a.Target != team.Target || a.TargetRevision != team.TargetRevision {
		return false, nil
	}
	r, err := s.readResult(*a)
	if err != nil || !repairableSkillFailure(r) || !s.safeResultContent(*a, r) {
		return false, nil
	}
	team.Diagnostic = "Ошибка структуры ответа: выполните обязательные проверки result_checks выбранного навыка. Математический вывод не подтвержден."
	if err := s.queueTeamAttempt(d, team, role, a.ID); err != nil {
		return false, err
	}
	team.FormatRepairsUsed++
	team.Reason = "Назначено ограниченное исправление формата с сохранением первоначального ответа."
	return true, nil
}
