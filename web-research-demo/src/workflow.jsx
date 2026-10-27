import React, { useEffect, useState } from "react";
import { Check, Pause, Play, Plus, Square } from "lucide-react";
import { api } from "./api.js";
import { TeamFields } from "./research-management.jsx";
import { PlanPreparationPanel } from "./plan-preparation.jsx";

const labels = {
  prepared: "Подготовлен",
  planning: "Планирование",
  awaiting_plan: "Подтверждение плана",
  needs_input: "Нужны ответы",
  running: "Исследование",
  awaiting_acceptance: "Отдельная приемка",
  needs_operator: "Нужно решение",
  paused: "Приостановлен",
  completed: "Завершен",
  stopped: "Остановлен",
  invalidated: "Постановка изменена",
};
const closed = (w) =>
  !w || ["completed", "stopped", "invalidated"].includes(w.status);
const roles = ["proof", "counterexample", "formalize", "review"];

export function DecisionForm({ live, title, route, kind, onDone, onError }) {
  return (
    <form
      className="runtime-form"
      aria-label={title}
      onSubmit={async (e) => {
        e.preventDefault();
        const f = Object.fromEntries(new FormData(e.currentTarget));
        try {
          await live.command(route, {
            kind,
            note: f.note,
            confirm: f.confirm === "on",
          });
          onDone();
        } catch (err) {
          onError(err.message);
        }
      }}
    >
      <label className="wide">
        Основание
        <textarea name="note" required maxLength={4000} />
      </label>
      <label className="checkbox-label wide">
        <input type="checkbox" name="confirm" required />
        {title}
      </label>
      <button className="button primary" disabled={live.busy}>
        <Check size={15} />
        Подтвердить
      </button>
      <button type="button" className="button secondary" onClick={onDone}>
        Отмена
      </button>
    </form>
  );
}

export function WorkflowPanel({ live, study }) {
  const [open, setOpen] = useState(false),
    [decision, setDecision] = useState(""),
    [error, setError] = useState("");
  const state = live.data.state,
    st = state.studies.find((s) => s.id === study),
    w = st?.workflow;
  const proposal = state.proposals?.find((p) => p.id === w?.proposal);
  useEffect(() => {
    setOpen(false);
    setDecision("");
    setError("");
  }, [study]);
  return (
    <section
      className="process-section"
      aria-label="Полный исследовательский цикл"
    >
      <div className="section-intro">
        <h2>Полный цикл</h2>
        <button
          className="button secondary"
          disabled={live.busy || !closed(w)}
          onClick={() => setOpen(!open)}
        >
          <Plus size={15} />
          Подготовить
        </button>
      </div>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {open && (
        <form
          className="runtime-form"
          aria-label="Подготовка полного цикла"
          onSubmit={async (e) => {
            e.preventDefault();
            const f = Object.fromEntries(new FormData(e.currentTarget));
            setError("");
            try {
              await live.command("/workflows", {
                study,
                planner: f.planner,
                confirm: f.confirm === "on",
                team: {
                  profiles: Object.fromEntries(roles.map((r) => [r, f[r]])),
                  workspace: f.workspace,
                  max_attempts: Number(f.max_attempts),
                  format_repair_limit: Number(f.format_repair_limit),
                  strategy: f.strategy,
                  require_lean: true,
                },
              });
              setOpen(false);
            } catch (err) {
              setError(err.message);
            }
          }}
        >
          <label>
            Планировщик
            <select name="planner" required>
              {live.data.profiles.map((p) => (
                <option key={p.id} value={p.id} disabled={!p.available}>
                  {p.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            Распределение
            <select name="strategy" defaultValue="demand">
              <option value="demand">Специалисты по необходимости</option>
              <option value="fixed">Фиксированные роли</option>
              <option value="adaptive">Сначала контрпримеры</option>
            </select>
          </label>
          <TeamFields live={live} />
          <label className="checkbox-label wide">
            <input name="confirm" type="checkbox" required />
            Подтверждаю расходы в общем бюджете исследования
          </label>
          <button className="button primary" disabled={live.busy}>
            <Check size={15} />
            Подготовить цикл
          </button>
        </form>
      )}
      {w && (
        <div className="team-row">
          <div className="section-intro">
            <strong>{labels[w.status] || w.status}</strong>
            <span>Предложений плана: {w.planning_round} / 2</span>
          </div>
          <p>{w.reason}</p>
          {proposal && <PlanPreparationPanel live={live} proposal={proposal} />}
          {(w.question_ids || []).map((id) => {
            const q = state.questions.find((q) => q.id === id);
            if (!q) return null;
            return (
              <div key={id}>
                <strong>{q.text}</strong>
                {q.answer ? (
                  <p>{q.answer}</p>
                ) : (
                  <form
                    className="runtime-form"
                    aria-label="Ответ планировщику"
                    onSubmit={async (e) => {
                      e.preventDefault();
                      const f = Object.fromEntries(
                        new FormData(e.currentTarget),
                      );
                      try {
                        await live.command(
                          `/questions/${encodeURIComponent(id)}/workflow-answer`,
                          { answer: f.answer, confirm: f.confirm === "on" },
                        );
                      } catch (err) {
                        setError(err.message);
                      }
                    }}
                  >
                    <label className="wide">
                      Ответ
                      <textarea name="answer" required maxLength={4000} />
                    </label>
                    <label className="checkbox-label">
                      <input name="confirm" type="checkbox" required />
                      Подтверждаю уточнение условий
                    </label>
                    <button
                      className="button primary"
                      disabled={live.busy || closed(w)}
                    >
                      <Check size={15} />
                      Записать ответ
                    </button>
                  </form>
                )}
              </div>
            );
          })}
          {proposal && (
            <details>
              <summary>Леммы и покрытие</summary>
              <p>{proposal.report.summary}</p>
              {[...proposal.report.claims, proposal.report.coverage].map(
                (c, i) => (
                  <div key={i}>
                    <strong>{c.title}</strong>
                    <p>{c.statement}</p>
                    <p>{c.assumptions}</p>
                    {c.formal_goal && (
                      <pre className="source-preview">
                        {c.formal_goal.source}
                      </pre>
                    )}
                  </div>
                ),
              )}
            </details>
          )}
          {!closed(w) && (
            <div className="action-row">
              {w.status === "awaiting_plan" && (
                <button
                  className="button primary"
                  disabled={
                    live.busy || proposal?.preparation?.status !== "ready"
                  }
                  onClick={() => setDecision("approve_plan")}
                >
                  <Check size={15} />
                  Утвердить план
                </button>
              )}
              {["paused", "needs_input", "needs_operator"].includes(
                w.status,
              ) ? (
                <button
                  className="button secondary"
                  disabled={live.busy}
                  onClick={() => setDecision("resume")}
                >
                  <Play size={15} />
                  Продолжить
                </button>
              ) : (
                <button
                  className="button secondary"
                  disabled={live.busy}
                  onClick={() => setDecision("pause")}
                >
                  <Pause size={15} />
                  Пауза
                </button>
              )}
              <button
                className="button secondary"
                disabled={live.busy}
                onClick={() => setDecision("stop")}
              >
                <Square size={15} />
                Остановить
              </button>
            </div>
          )}
        </div>
      )}
      {decision && (
        <DecisionForm
          live={live}
          title={
            {
              approve_plan: "Подтверждаю постановки, покрытие и запуск ветвей",
              pause: "Подтверждаю паузу цикла",
              resume: "Подтверждаю продолжение в прежнем бюджете",
              stop: "Подтверждаю остановку цикла",
            }[decision]
          }
          route={`/studies/${encodeURIComponent(study)}/workflow`}
          kind={decision}
          onDone={() => setDecision("")}
          onError={setError}
        />
      )}
    </section>
  );
}

export function QueuePanel({ live, study }) {
  const [rows, setRows] = useState([]),
    [error, setError] = useState(""),
    [decision, setDecision] = useState(null);
  useEffect(() => {
    let active = true;
    api(`/queue?study=${encodeURIComponent(study || "")}`)
      .then((v) => {
        if (active) {
          setRows(v);
          setError("");
        }
      })
      .catch((e) => {
        if (active) setError(e.message);
      });
    return () => {
      active = false;
    };
  }, [study, live.data.state.revision]);
  return (
    <section className="process-section" aria-label="Сохраненная очередь">
      <h2>Очередь</h2>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {!rows.length && <p>Нет ожидающих заданий.</p>}
      {rows.map((r) => (
        <div className="team-row" key={r.attempt}>
          <div className="section-intro">
            <strong>
              {live.data.state.entities.find((e) => e.id === r.target)?.title ||
                r.target}
            </strong>
            <span>
              {r.requires_new_assignment
                ? "Нужно отдельное назначение"
                : r.held
                  ? "Удержано"
                  : r.status === "queued"
                    ? "В очереди"
                    : "В работе"}
            </span>
          </div>
          <p>{r.reason}</p>
          {r.status === "queued" && r.remote_outcome === "not_started" && (
            <div className="action-row">
              {!r.held && (
                <button
                  className="button secondary"
                  disabled={live.busy}
                  onClick={() => setDecision({ id: r.attempt, kind: "hold" })}
                >
                  <Pause size={15} />
                  Удержать
                </button>
              )}
              {r.can_release && (
                <button
                  className="button secondary"
                  disabled={live.busy}
                  onClick={() =>
                    setDecision({ id: r.attempt, kind: "release" })
                  }
                >
                  <Play size={15} />
                  Освободить
                </button>
              )}
              <button
                className="button secondary"
                disabled={live.busy}
                onClick={() => setDecision({ id: r.attempt, kind: "cancel" })}
              >
                <Square size={15} />
                Отменить
              </button>
            </div>
          )}
        </div>
      ))}
      {decision && (
        <DecisionForm
          live={live}
          title="Подтверждаю изменение очереди"
          route={`/attempts/${encodeURIComponent(decision.id)}/queue`}
          kind={decision.kind}
          onDone={() => setDecision(null)}
          onError={setError}
        />
      )}
    </section>
  );
}
