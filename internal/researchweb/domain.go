package researchweb

import "strings"

func findingTextMatches(f Finding, text string) bool {
	if f.Text == text || f.SourceTextSHA256 == hash(text) {
		return true
	}
	// Earlier records appended the resolution without a separate source digest.
	if f.SourceTextSHA256 == "" && f.State == "resolved" {
		original, _, ok := strings.Cut(f.Text, "\nРезультат перепроверки: ")
		return ok && original == text
	}
	return false
}

func actionLabel(kind string) string {
	labels := map[string]string{
		"CREATE_STUDY": "Создано исследование", "LEMMA": "Добавлена лемма-кандидат", "SPLIT": "Предложено разбиение",
		"QUESTION": "Записано сообщение", "COUNTEREXAMPLE": "Создано задание поиска контрпримера", "TASK": "Создано задание",
		"APPLY": "Предложено применение леммы", "SUBMIT_REVIEW": "Запрошена рецензия", "REVIEW": "Записано решение рецензента",
		"FINDING": "Добавлено замечание", "CHALLENGE": "Оспорено основание", "RESOLVE_FINDING": "Запрошена перепроверка",
		"CLOSE_FINDING": "Записан результат перепроверки", "PAUSE": "Изменен допуск новых запусков", "ATTACH_PROOF": "Материал попытки направлен на приемку",
	}
	return labels[kind]
}
func applyAction(d *Data, a Action) error {
	if actionLabel(a.Type) == "" {
		return RuleError("Неизвестное действие.")
	}
	if len(a.Text) > 64000 || len(a.Title) > 300 || len(a.Statement) > 16000 || len(a.Assumptions) > 8000 || len(a.Parts) > 2 {
		return ErrLimit
	}
	item := d.entity(a.Target)
	switch a.Type {
	case "CREATE_STUDY":
		if !textOK(a.Title, 300) || !textOK(a.Statement, 16000) || !textOK(a.Assumptions, 8000) {
			return RuleError("Нужны название, утверждение и условия.")
		}
		id, goal := identifier("S"), identifier("H")
		d.Studies = append(d.Studies, Study{ID: id, Title: a.Title, Goal: goal, Category: a.Category})
		d.Entities = append(d.Entities, newEntity(goal, a.Title, "goal", id, a.Statement, a.Assumptions))
	case "PAUSE":
		d.Paused = !d.Paused
	case "LEMMA":
		found := false
		for _, s := range d.Studies {
			found = found || s.ID == a.Study
		}
		if !found || !textOK(a.Title, 300) || !textOK(a.Statement, 16000) || !textOK(a.Assumptions, 8000) {
			return RuleError("Выберите исследование и заполните лемму.")
		}
		d.Entities = append(d.Entities, newEntity(identifier("L"), a.Title, "lemma", a.Study, a.Statement, a.Assumptions))
	case "SPLIT":
		if item == nil || item.Status == "accepted" || item.Status == "refuted" {
			return RuleError("Принятую или опровергнутую версию нельзя менять разбиением.")
		}
		if len(a.Parts) != 2 || !textOK(a.Parts[0], 1000) || !textOK(a.Parts[1], 1000) {
			return RuleError("Нужны два непустых случая.")
		}
		parent := *item
		ids := []string{identifier("C"), identifier("C"), identifier("O")}
		item.Dependencies = append(item.Dependencies, ids...)
		item.Revision++
		item.Status = "open"
		item.Proof = ""
		item.ProofAuthor = ""
		item.ProofAttempt = ""
		item.ProofVerification = ""
		item.DependencyRevisions = nil
		for i, title := range a.Parts {
			d.Entities = append(d.Entities, newEntity(ids[i], title, "claim", parent.Study, title, parent.Assumptions))
			d.WorkLinks = append(d.WorkLinks, [2]string{parent.ID, ids[i]})
		}
		d.Entities = append(d.Entities, newEntity(ids[2], "Покрытие разбиения", "obligation", parent.Study, "Доказать, что случаи покрывают исходную область.", parent.Assumptions))
		d.WorkLinks = append(d.WorkLinks, [2]string{parent.ID, ids[2]})
	case "TASK", "COUNTEREXAMPLE":
		if item == nil {
			return RuleError("Утверждение не найдено.")
		}
		kind, objective, title := a.Kind, a.Text, a.Title
		if a.Type == "COUNTEREXAMPLE" {
			kind = "counterexample"
			title = "Поиск контрпримера: " + item.Title
			objective = "Проверить граничные случаи и найти контрпример, удовлетворяющий всем предпосылкам."
		}
		if kind != "proof" && kind != "review" && kind != "counterexample" && kind != "formalize" && kind != "decompose" {
			return RuleError("Неподдерживаемый вид задания.")
		}
		if !textOK(title, 500) || !textOK(objective, 16000) {
			return RuleError("Нужны название и цель задания.")
		}
		d.addTask(item.ID, title, kind, objective)
	case "QUESTION":
		if item == nil || !textOK(a.Text, 4000) || (a.Kind != "" && a.Kind != "question" && a.Kind != "proposal") {
			return RuleError("Укажите вопрос и объект обсуждения.")
		}
		id := identifier("Q")
		d.Questions = append(d.Questions, Question{ID: id, Target: item.ID, Text: strings.TrimSpace(a.Text), Kind: a.Kind, Snapshot: d.Revision})
		if a.Kind != "proposal" {
			d.addTask(item.ID, "Ответ на вопрос", "answer", a.Text)
			d.Tasks[len(d.Tasks)-1].Question = id
		}
	case "APPLY":
		lemma := d.entity(a.Lemma)
		published := false
		for _, l := range d.Library {
			if l.Lemma == a.Lemma && l.Status == "ready" && libraryMatches(d, l) {
				published = true
			}
		}
		if lemma == nil || (lemma.Kind != "lemma" && !published) || d.effective(lemma.ID, map[string]bool{}) != "accepted" {
			return RuleError("Нужна действующая принятая лемма.")
		}
		if item == nil || item.ID == lemma.ID || (item.Kind != "goal" && item.Kind != "claim") || item.Status == "accepted" || item.Status == "refuted" {
			return RuleError("Выберите открытое целевое утверждение.")
		}
		for _, app := range d.Applications {
			if app.Lemma == lemma.ID && app.Target == item.ID {
				return RuleError("Применение уже предложено.")
			}
		}
		id := identifier("A")
		app := Application{ID: id, Lemma: lemma.ID, LemmaRevision: lemma.Revision, Target: item.ID, State: "candidate"}
		obligation := newEntity(id, "Применимость "+lemma.ID, "application", item.Study, "Проверить предпосылки и перенос результата "+lemma.ID+" к "+item.ID+".", item.Assumptions)
		obligation.Dependencies = []string{lemma.ID}
		item.Dependencies = append(item.Dependencies, id)
		item.Revision++
		item.Proof = ""
		item.ProofAuthor = ""
		item.ProofAttempt = ""
		item.ProofVerification = ""
		item.DependencyRevisions = nil
		d.WorkLinks = append(d.WorkLinks, [2]string{item.ID, id})
		d.Applications = append(d.Applications, app)
		d.Entities = append(d.Entities, obligation)
		d.addTask(id, "Проверить перенос леммы", "proof", obligation.Statement)
	case "SUBMIT_REVIEW":
		if item == nil || (item.Status != "open" && item.Status != "needs_changes" && item.Status != "challenged") {
			return RuleError("Кандидат недоступен для рецензии.")
		}
		if !textOK(item.Proof, 64000) {
			return RuleError("Сначала добавьте материал доказательства из завершенной попытки.")
		}
		item.Status = "in_review"
		d.addTask(item.ID, "Рецензия: "+item.Title, "review", "Проверить каждый переход, предпосылки и незакрытые обязательства.")
	case "REVIEW":
		if item == nil || item.Status != "in_review" || !textOK(a.Text, 4000) {
			return RuleError("Нужен кандидат на проверке и обоснование решения.")
		}
		if a.Decision != "accept" && a.Decision != "reject" {
			return RuleError("Неизвестное решение.")
		}
		if a.Decision == "accept" {
			if d.effective(item.ID, map[string]bool{}) == "blocked" {
				return RuleError("Изменились версии оснований.")
			}
			if item.ProofAuthor == "" || item.ProofAuthor == "operator" || item.Proof == "" {
				return RuleError("Автор доказательства не может принять собственный материал.")
			}
			for _, f := range d.Findings {
				if f.Target == item.ID && f.Severity != "editorial" && f.State != "resolved" {
					return RuleError("Есть незакрытые математические замечания.")
				}
			}
			for _, dep := range item.Dependencies {
				if d.effective(dep, map[string]bool{}) != "accepted" {
					return RuleError("Есть непринятые или оспоренные основания.")
				}
			}
			for i, app := range d.Applications {
				if app.ID == item.ID {
					lemma := d.entity(app.Lemma)
					if lemma == nil || lemma.Revision != app.LemmaRevision {
						return RuleError("Изменилась версия применяемой леммы.")
					}
					d.Applications[i].State = "accepted"
				}
			}
			item.Status = "accepted"
		} else {
			item.Status = "needs_changes"
		}
		item.ReviewReason = a.Text
		item.ReviewedBy = "operator"
	case "FINDING", "CHALLENGE":
		if item == nil || !textOK(a.Text, 4000) {
			return RuleError("Нужно основание замечания.")
		}
		severity := a.Severity
		if severity == "" {
			severity = "question"
		}
		if severity != "major" && severity != "editorial" && severity != "question" {
			return RuleError("Неизвестная категория замечания.")
		}
		if a.Type == "CHALLENGE" {
			if item.Status != "accepted" {
				return RuleError("Можно оспорить только принятую запись.")
			}
			item.Status = "challenged"
			severity = "major"
		}
		if item.Status == "accepted" && severity == "major" {
			item.Status = "challenged"
		}
		d.Findings = append(d.Findings, Finding{ID: identifier("F"), Target: item.ID, Text: a.Text, Severity: severity, State: "open", Revision: item.Revision})
	case "RESOLVE_FINDING", "CLOSE_FINDING":
		for i := range d.Findings {
			f := &d.Findings[i]
			if f.ID == a.Finding {
				if a.Type == "RESOLVE_FINDING" {
					if f.State != "open" {
						return RuleError("Замечание уже направлено на перепроверку.")
					}
					f.State = "verification_pending"
				} else {
					if f.State != "verification_pending" || !textOK(a.Text, 4000) {
						return RuleError("Нужен результат перепроверки замечания.")
					}
					f.State = "resolved"
					if f.ReviewAttempt != "" && f.SourceTextSHA256 == "" {
						f.SourceTextSHA256 = hash(f.Text)
					}
					f.Text += "\nРезультат перепроверки: " + a.Text
				}
				return nil
			}
		}
		return RuleError("Замечание не найдено.")
	default:
		return RuleError("Материал попытки обрабатывается отдельной командой.")
	}
	return nil
}
