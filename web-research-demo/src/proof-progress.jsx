import React, { useEffect, useState } from "react";
import { Plus, FlaskConical, ChevronRight } from "lucide-react";
import { api } from "./api.js";
import { ResearchAudit } from "./proof-audit.jsx";

const statusNames = {
  open: "Открыто",
  blocked: "Ожидает оснований",
  accepted: "Принято",
  refuted: "Опровергнуто",
  in_review: "На рецензии",
  needs_changes: "Требует исправлений",
  stale: "Устарело",
};

export function ProofProgressPanel({ live, study, onSelect }) {
  const [progress, setProgress] = useState(null);
  const [completion, setCompletion] = useState(null);
  const [error, setError] = useState("");
  const [route, setRoute] = useState("decomposed");
  const revision = live.data.state.revision;
  useEffect(() => {
    let active = true;
    setProgress((old) => (old?.study === study ? old : null));
    if (study)
      api(`/studies/${study}/proof-progress`)
        .then((value) => {
          if (active) {
            setProgress(value);
            setError("");
          }
        })
        .catch((e) => {
          if (active) setError(e.message);
        });
    if (study)
      api(`/studies/${study}/completion`)
        .then((value) => {
          if (active) setCompletion(value);
        })
        .catch(() => {
          if (active) setCompletion(null);
        });
    return () => {
      active = false;
    };
  }, [study, revision]);
  async function tactics(id) {
    if (
      !window.confirm(
        "Назначить пять местных проверок Lean без обращения к модели?",
      )
    )
      return;
    try {
      setError("");
      await live.command(`/entities/${id}/tactics`, {
        max_trials: 5,
        confirm: true,
      });
    } catch (e) {
      setError(e.message);
    }
  }
  return (
    <section
      className="process-section"
      aria-label="Обязательства доказательства"
    >
      <div className="section-intro">
        <h3>Обязательства доказательства</h3>
        <label>
          Постановка учебного исследования
          <select
            aria-label="Постановка учебного исследования"
            value={route}
            onChange={(e) => setRoute(e.target.value)}
          >
            <option value="decomposed">С разбиением на леммы</option>
            <option value="direct">Прямая цель</option>
          </select>
        </label>
        <button
          className="button secondary"
          disabled={live.busy || !live.data.lean?.configured}
          onClick={async () => {
            if (
              !window.confirm(
                "Создать учебное исследование инъективности итераций без запуска модели?",
              )
            )
              return;
            try {
              setError("");
              await live.command("/research-pilots", {
                recipe: "injective-iterations@1",
                route,
                confirm: true,
              });
            } catch (e) {
              setError(e.message);
            }
          }}
        >
          <Plus size={15} />
          Учебное исследование
        </button>
      </div>
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
      {study && (
        <label className="checkbox-label">
          <input
            type="checkbox"
            disabled={live.busy}
            checked={
              !!live.data.state.studies.find((s) => s.id === study)
                ?.pause_on_ready
            }
            onChange={async (e) => {
              const value = e.target.checked;
              if (
                !window.confirm(
                  "Изменить приостановку поиска при готовности основной цели? Приемка остается отдельным решением.",
                )
              )
                return;
              try {
                setError("");
                await live.command(`/studies/${study}/policy`, {
                  pause_on_ready: value,
                  confirm: true,
                });
              } catch (error) {
                setError(error.message);
              }
            }}
          />
          Приостанавливать поиск при готовности основной цели
        </label>
      )}
      {progress && (
        <>
          {completion && (
            <div className="usage-summary" aria-label="Итог исследования">
              <p>
                {completion.status === "completed"
                  ? "Основная цель закрыта"
                  : completion.status === "awaiting_operator"
                    ? "Ожидает решения оператора"
                    : "Основная цель открыта"}
                . Активных попыток: {completion.active_attempts}.
              </p>
              <p>
                {completion.search_suspended &&
                  "Поиск приостановлен до решения оператора. "}
                Исходники исполнителей: {completion.executor_sources}.
                Представленные исходники: {completion.submitted_sources}.
                Перенесенные: {completion.imported_sources}. Заметки оператора:{" "}
                {completion.operator_notes}. Математических подсказок:{" "}
                {completion.mathematical_hints || 0}. Неклассифицированных
                заметок: {completion.unclassified_notes || 0}.
              </p>
            </div>
          )}
          <div className="table-wrap">
            <table className="proof-progress-table">
              <thead>
                <tr>
                  <th>Утверждение</th>
                  <th>Состояние</th>
                  <th>Lean</th>
                  <th>Основания и замечания</th>
                  <th>Действия</th>
                </tr>
              </thead>
              <tbody>
                {progress.nodes.map((n) => (
                  <tr key={n.id}>
                    <td>{n.title}</td>
                    <td>{statusNames[n.status] || n.status}</td>
                    <td>
                      {n.native === "verified"
                        ? "Проверено"
                        : n.native === "none"
                          ? "Нет проверки"
                          : n.native}
                    </td>
                    <td>
                      {n.blockers.length ? (
                        <ul>
                          {n.blockers.map((b, i) => (
                            <li key={i}>{b}</li>
                          ))}
                        </ul>
                      ) : (
                        "Нет незакрытых оснований"
                      )}
                      {n.modules.map((m) => (
                        <p key={m}>{m}</p>
                      ))}
                    </td>
                    <td>
                      <div className="row-actions">
                        {onSelect && (
                          <button
                            className="button secondary"
                            title="Открыть утверждение"
                            aria-label={`Открыть: ${n.title}`}
                            onClick={() => onSelect(n.id)}
                          >
                            <ChevronRight size={16} />
                          </button>
                        )}
                        {!["accepted", "refuted"].includes(n.status) && (
                          <button
                            className="button secondary"
                            title="Отдельные приемы Lean"
                            aria-label={`Проверить Lean: ${n.title}`}
                            disabled={live.busy}
                            onClick={() => tactics(n.id)}
                          >
                            <FlaskConical size={16} />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <ResearchAudit
            key={study}
            progress={progress}
            state={live.data.state}
            onSelect={onSelect}
          />
        </>
      )}
    </section>
  );
}
