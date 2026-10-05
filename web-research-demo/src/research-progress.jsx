import React, { useEffect, useState } from "react";
import {
  Search,
  Plus,
  Save,
  Square,
  ShieldCheck,
  Send,
  RotateCcw,
} from "lucide-react";
import { api } from "./api.js";
import { verificationCurrent } from "./proof-state.js";

const statusNames = {
  ready: "Ожидает",
  running: "В работе",
  blocked: "Ожидает уточнения",
  awaiting_review: "Ожидает приемки",
  completed: "Завершена",
  invalidated: "Устарела",
  abandoned: "Остановлена",
  paused: "Приостановлена",
  cancelled: "Остановлена",
};
const trustNames = {
  accepted_lemma: "Принятая лемма",
  verified_refutation: "Проверенное опровержение",
  recorded_failure: "Неудачная попытка",
  unverified_note: "Непроверенная заметка",
  stale: "Устарело",
};

export function BranchList({ live, study, all, methods = [] }) {
  const [error, setError] = useState("");
  const branches = (live.data.state.branches || []).filter(
    (b) => all || b.study === study,
  );
  if (!branches.length) return null;
  async function command(b, kind, priority) {
    const note = window.prompt("Основание решения");
    if (!note?.trim()) return;
    try {
      setError("");
      await live.command(`/branches/${b.id}/commands`, {
        kind,
        priority,
        note,
        confirm: true,
      });
    } catch (e) {
      setError(e.message);
    }
  }
  return (
    <div className="branch-list" aria-label="Ветви исследования">
      <h3>Ветви исследования</h3>
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
      {branches.map((b) => (
        <div className="team-row" key={b.id}>
          <div className="section-intro">
            <strong>
              {methods.find((m) => m.id === b.method)?.label || b.method} ·{" "}
              {live.data.state.entities.find((e) => e.id === b.target)?.title}
            </strong>
            <span>{statusNames[b.status] || b.status}</span>
          </div>
          <p>{b.rationale}</p>
          <p>{b.reason}</p>
          <div className="row-actions">
            <label>
              Приоритет{" "}
              <input
                aria-label={`Приоритет ${b.method}`}
                type="number"
                min="0"
                max="100"
                defaultValue={b.priority}
                disabled={b.status !== "ready" || live.busy}
                onBlur={(e) => {
                  if (Number(e.target.value) !== b.priority)
                    command(b, "prioritize", Number(e.target.value));
                }}
              />
            </label>
            {b.status === "blocked" && !b.team && (
              <button
                className="button secondary"
                disabled={live.busy}
                onClick={() => command(b, "resume", b.priority)}
              >
                <RotateCcw size={15} />
                Продолжить
              </button>
            )}
            {!["completed", "invalidated", "abandoned", "cancelled"].includes(
              b.status,
            ) && (
              <button
                className="button secondary"
                disabled={live.busy}
                onClick={() => command(b, "abandon", b.priority)}
              >
                <Square size={15} />
                Остановить ветвь
              </button>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

export function RefutationActions({ live, entity, verification: v }) {
  const [error, setError] = useState("");
  const reviews = live.data.state.attempts.filter(
    (a) =>
      a.target === entity.id &&
      a.status === "candidate" &&
      a.proof_binding?.verification === v.id,
  );
  if (
    v.status !== "verified" ||
    !verificationCurrent(v, entity, live.data.state) ||
    ["accepted", "refuted"].includes(entity.status)
  )
    return null;
  return (
    <div>
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
      <form
        className="runtime-form"
        aria-label="Приемка опровержения"
        onSubmit={async (e) => {
          e.preventDefault();
          const data = Object.fromEntries(new FormData(e.currentTarget));
          setError("");
          try {
            if (!entity.refutation_review)
              await live.command(`/refutations/${v.id}/review`, {
                review_attempt: data.review,
              });
            else
              await live.command(`/refutations/${v.id}/accept`, {
                confirm: data.confirm === "on",
                note: data.note,
              });
          } catch (err) {
            setError(err.message);
          }
        }}
      >
        {!entity.refutation_review ? (
          <>
            <label className="wide">
              Рецензия
              <select name="review" required defaultValue="">
                <option value="" disabled>
                  Выберите связанную рецензию
                </option>
                {reviews.map((a) => (
                  <option value={a.id} key={a.id}>
                    {a.profile} · {a.id}
                  </option>
                ))}
              </select>
            </label>
            <button
              className="button secondary"
              disabled={live.busy || !reviews.length}
            >
              <Send size={15} />
              Связать рецензию
            </button>
          </>
        ) : (
          <>
            <label className="wide">
              Основание приемки
              <textarea name="note" required maxLength="4000" />
            </label>
            <label className="checkbox wide">
              <input type="checkbox" name="confirm" required />
              Подтверждаю опровержение закрепленного утверждения
            </label>
            <button className="button primary" disabled={live.busy}>
              <ShieldCheck size={15} />
              Принять опровержение
            </button>
          </>
        )}
      </form>
    </div>
  );
}

export function MemoryPanel({ live, study, selected, onSelect }) {
  const [query, setQuery] = useState(""),
    [other, setOther] = useState(false),
    [hits, setHits] = useState([]),
    [error, setError] = useState(""),
    [open, setOpen] = useState(false),
    [kind, setKind] = useState("note");
  const state = live.data.state;
  const entity =
    state.entities.find((e) => e.id === selected && e.study === study) ||
    state.entities.find(
      (e) => e.id === state.studies.find((s) => s.id === study)?.goal,
    );
  useEffect(() => {
    if (!study) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      api(
        `/memory/search?${new URLSearchParams({ q: query, study, other_studies: String(other) })}`,
        { signal: controller.signal },
      )
        .then((v) => {
          setHits(v.hits);
          setError("");
        })
        .catch((e) => {
          if (e.name !== "AbortError") setError(e.message);
        });
    }, 150);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [study, query, other, state.revision]);
  return (
    <section className="process-section" aria-label="Память исследования">
      <div className="section-intro">
        <h2>Память исследования</h2>
        <button
          className="button secondary"
          disabled={live.busy || !entity}
          onClick={() => setOpen(!open)}
        >
          <Plus size={15} />
          Добавить запись
        </button>
      </div>
      <div className="runtime-form">
        <label>
          Поиск
          <input
            aria-label="Поиск в памяти"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            maxLength="300"
            type="search"
          />
        </label>
        <label className="checkbox">
          <input
            type="checkbox"
            checked={other}
            onChange={(e) => setOther(e.target.checked)}
          />
          Леммы других исследований
        </label>
      </div>
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
      {open && entity && (
        <form
          className="runtime-form"
          aria-label="Запись в память"
          onSubmit={async (e) => {
            e.preventDefault();
            const v = Object.fromEntries(new FormData(e.currentTarget));
            setError("");
            try {
              await live.command("/memory", {
                target: entity.id,
                kind,
                content: v.content,
                conditions: v.conditions,
                ...(kind === "failed_method"
                  ? { attempt: v.attempt, check: v.check }
                  : {}),
                ...(kind === "counterexample"
                  ? { verification: entity.refutation_verification }
                  : {}),
              });
              setOpen(false);
            } catch (err) {
              setError(err.message);
            }
          }}
        >
          <label>
            Вид
            <select value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="note">Заметка</option>
              <option value="failed_method">Неудачный прием</option>
              <option
                value="counterexample"
                disabled={
                  entity.status !== "refuted" || !entity.refutation_verification
                }
              >
                Проверенный контрпример
              </option>
            </select>
          </label>
          {kind === "failed_method" && (
            <>
              <label>
                Попытка
                <select name="attempt" required>
                  {state.attempts
                    .filter(
                      (a) =>
                        a.target === entity.id &&
                        a.target_revision === entity.revision &&
                        ![
                          "queued",
                          "running",
                          "preparing",
                          "awaiting_worker",
                          "cancelling",
                        ].includes(a.status),
                    )
                    .map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.profile} · {a.id}
                      </option>
                    ))}
                </select>
              </label>
              <label>
                Идентификатор неудачной проверки
                <input name="check" maxLength="100" />
              </label>
            </>
          )}
          <label className="wide">
            Содержание
            <textarea name="content" required maxLength="4000" />
          </label>
          <label className="wide">
            Условия применимости
            <textarea name="conditions" maxLength="4000" />
          </label>
          <button className="button primary" disabled={live.busy}>
            <Save size={15} />
            Записать
          </button>
        </form>
      )}
      <div className="memory-list">
        {hits.map((h) => (
          <article className="team-row" key={h.id}>
            <div className="section-intro">
              <strong>{trustNames[h.trust] || h.trust}</strong>
              <span>Версия {h.revision}</span>
            </div>
            <p>{h.content}</p>
            {h.conditions && <p>{h.conditions}</p>}
            {h.module && <code>{h.module}</code>}
            <button
              className="button secondary"
              onClick={() => onSelect?.(h.target)}
            >
              <Search size={15} />
              Открыть утверждение
            </button>
          </article>
        ))}
        {!hits.length && <p>Записей не найдено.</p>}
      </div>
    </section>
  );
}
