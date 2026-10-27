package researchweb

import (
	"github.com/snow-ghost/research-team/internal/researchcompare"
	"net/http"
	"path/filepath"
	"strconv"
)

func (h *HTTP) researchProgressRoutes() {
	h.mux.HandleFunc("POST /api/evaluations/{id}/control", func(w http.ResponseWriter, r *http.Request) {
		var body EvaluationCommand
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.ControlEvaluation(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/questions/{id}/workflow-answer", func(w http.ResponseWriter, r *http.Request) {
		var body WorkflowAnswer
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.AnswerWorkflowQuestion(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/attempts/{id}/request-packet", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.service.Store.Read()
		if err != nil {
			h.fail(w, err)
			return
		}
		a := v.attempt(r.PathValue("id"))
		if a == nil {
			h.fail(w, RuleError("Попытка не найдена."))
			return
		}
		body, err := readBoundedFile(filepath.Join(h.service.Options.Config.DataDir, "attempts", a.ID, "request-packet.json"), 1<<20)
		if err != nil {
			h.fail(w, RuleError("Точный вход не сохранен для этой попытки; старые входы автоматически не восстанавливаются."))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	})
	h.mux.HandleFunc("POST /api/workflows", func(w http.ResponseWriter, r *http.Request) {
		var body WorkflowRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.CreateWorkflow(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/studies/{id}/workflow", func(w http.ResponseWriter, r *http.Request) {
		var body WorkflowCommand
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.ControlWorkflow(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/queue", func(w http.ResponseWriter, r *http.Request) {
		value, err := h.service.Queue(r.URL.Query().Get("study"))
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, value)
	})
	h.mux.HandleFunc("POST /api/attempts/{id}/queue", func(w http.ResponseWriter, r *http.Request) {
		var body QueueCommand
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.ControlQueue(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/evaluations/{id}/analysis", func(w http.ResponseWriter, r *http.Request) {
		for _, watch := range h.service.Options.Config.Evaluations {
			if watch.ID != r.PathValue("id") {
				continue
			}
			report, err := researchcompare.ReadReport(watch.File)
			if err != nil || report.Simulation || report.ConfigurationSHA256 != watch.ConfigurationSHA256 {
				h.fail(w, RuleError("Отчет наблюдаемого опыта недоступен."))
				return
			}
			value, err := researchcompare.Analyze(report, filepath.Dir(watch.File))
			if err != nil {
				h.fail(w, err)
				return
			}
			h.json(w, value)
			return
		}
		h.fail(w, RuleError("Опыт не найден."))
	})
	h.mux.HandleFunc("GET /api/comparisons/{id}/analysis", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.service.Store.Read()
		if err != nil {
			h.fail(w, err)
			return
		}
		for _, c := range v.Comparisons {
			if c.ID == r.PathValue("id") {
				value, err := researchcompare.Analyze(c.Report, "")
				if err != nil {
					h.fail(w, err)
					return
				}
				h.json(w, value)
				return
			}
		}
		h.fail(w, RuleError("Сравнение не найдено."))
	})
	h.mux.HandleFunc("GET /api/branches/recommendations", func(w http.ResponseWriter, r *http.Request) {
		value, err := h.service.BranchRecommendations(r.URL.Query().Get("study"))
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, value)
	})
	h.mux.HandleFunc("POST /api/branches/{id}/policy", func(w http.ResponseWriter, r *http.Request) {
		var body BranchPolicyRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.SetBranchPolicy(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/memory/suggestions", func(w http.ResponseWriter, r *http.Request) {
		value, err := h.service.MemorySuggestions(r.URL.Query().Get("target"))
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, value)
	})
	h.mux.HandleFunc("POST /api/memory/relations", func(w http.ResponseWriter, r *http.Request) {
		var body MemoryRelationRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.CreateMemoryRelation(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/experiments/representation", func(w http.ResponseWriter, r *http.Request) {
		var body RepresentationRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		value, err := h.service.CompareRepresentation(r.Context(), body)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, value)
	})
	h.mux.HandleFunc("POST /api/lemmas/reuse-trial", func(w http.ResponseWriter, r *http.Request) {
		var body ReuseTrialRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		value, err := h.service.CompareLemmaReuse(r.Context(), body)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, value)
	})
	h.mux.HandleFunc("POST /api/branches/{id}/decisions", func(w http.ResponseWriter, r *http.Request) {
		var body BranchDecisionRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.DecideBranch(r.PathValue("id"), body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("GET /api/evaluations", func(w http.ResponseWriter, r *http.Request) { h.json(w, h.service.EvaluationStatuses()) })
	h.mux.HandleFunc("GET /api/studies/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		after, through := 0, 0
		for name, dest := range map[string]*int{"after": &after, "through": &through} {
			if v := r.URL.Query().Get(name); v != "" {
				n, err := strconv.Atoi(v)
				if err != nil {
					h.fail(w, RuleError("Неверная граница истории."))
					return
				}
				*dest = n
			}
		}
		page, err := h.service.StudyHistory(r.PathValue("id"), after, through)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, page)
	})
	h.mux.HandleFunc("GET /api/studies/{id}/diff", func(w http.ResponseWriter, r *http.Request) {
		from, e1 := strconv.Atoi(r.URL.Query().Get("from"))
		to, e2 := strconv.Atoi(r.URL.Query().Get("to"))
		if e1 != nil || e2 != nil {
			h.fail(w, RuleError("Нужны два номера снимков."))
			return
		}
		diff, err := h.service.DiffStudy(r.PathValue("id"), r.URL.Query().Get("target"), from, to)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.json(w, diff)
	})
}
