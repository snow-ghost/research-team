package researchweb

import "net/http"

func (h *HTTP) reliabilityRoutes() {
	h.mux.HandleFunc("GET /api/attempts/{id}/counter-report", func(w http.ResponseWriter, r *http.Request) {
		v, err := h.service.Store.Read()
		if err != nil {
			h.fail(w, err)
			return
		}
		a := v.attempt(r.PathValue("id"))
		if a == nil || v.task(a.TaskID) == nil || v.task(a.TaskID).Kind != "counterexample" {
			h.fail(w, RuleError("Нужна попытка контрпримеров."))
			return
		}
		result, err := h.service.readResult(*a)
		if err != nil {
			h.fail(w, err)
			return
		}
		var report CounterReport
		if !h.service.safeResultContent(*a, result) || decodeAgentReport(result.Candidate, &report) != nil || validateCounterReport(report) != nil {
			h.fail(w, RuleError("Отчет не соответствует известной схеме."))
			return
		}
		issues, _ := counterObligations(report)
		auditable := len(issues) > 0
		for _, issue := range issues {
			if issue.Kind == "mathematical" {
				auditable = false
			}
		}
		h.json(w, map[string]any{"report": report, "report_sha256": hash(report), "issues": issues, "cleared": counterCleared(&v.Data, a, report), "auditable": auditable})
	})
	h.mux.HandleFunc("POST /api/counter-audits", func(w http.ResponseWriter, r *http.Request) {
		var body CounterAuditRequest
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.AuditCounter(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
	h.mux.HandleFunc("POST /api/providers/commands", func(w http.ResponseWriter, r *http.Request) {
		var body ProviderCommand
		if err := bodyJSON(w, r, &body); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.service.ControlProvider(body); err != nil {
			h.fail(w, err)
			return
		}
		h.view(w)
	})
}
