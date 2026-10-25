package researchweb

import (
	"net/http"
	"strconv"
)

func (h *HTTP) researchProgressRoutes() {
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
