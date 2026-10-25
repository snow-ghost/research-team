import React, { useEffect, useState } from "react";
import { GitCompareArrows, History, ChevronDown } from "lucide-react";

const labels = {
  title: "Название",
  statement: "Утверждение",
  assumptions: "Предпосылки",
  formal_goal: "Формальная цель",
  dependencies: "Основания",
  required_libraries: "Обязательные ссылки",
  status: "Состояние",
  proof: "Исходник доказательства",
  proof_author: "Автор",
  proof_attempt: "Попытка автора",
  proof_verification: "Проверка исходника",
  research_result: "Результат",
};
const show = (value) =>
  value === null
    ? "Отсутствует"
    : typeof value === "string"
      ? value
      : JSON.stringify(value, null, 2);

export function ResearchHistory({ live, study, onOpen }) {
  const revision = live.data.state.revision;
  const [events, setEvents] = useState([]),
    [error, setError] = useState(""),
    [older, setOlder] = useState(null),
    [loading, setLoading] = useState(false);
  const [from, setFrom] = useState(Math.max(1, revision - 1)),
    [to, setTo] = useState(revision),
    [target, setTarget] = useState(""),
    [diff, setDiff] = useState(null);
  const entities = live.data.state.entities.filter((e) => e.study === study);
  useEffect(() => {
    setDiff(null);
    setTarget("");
    setFrom(Math.max(1, revision - 1));
    setTo(revision);
  }, [study]);
  useEffect(() => {
    if (!study) return;
    let active = true;
    const after = Math.max(0, revision - 200);
    live
      .get(`/studies/${study}/history?after=${after}&through=${revision}`)
      .then((value) => {
        if (active) {
          setEvents(value.events);
          setOlder(after > 0 ? { after: 0, through: after } : null);
          setError("");
        }
      })
      .catch((e) => {
        if (active) setError(e.message);
      });
    return () => {
      active = false;
    };
  }, [study, revision, live.get]);
  async function earlier() {
    if (!older) return;
    setLoading(true);
    try {
      const value = await live.get(
        `/studies/${study}/history?after=${older.after}&through=${older.through}`,
      );
      setEvents((old) => [
        ...new Map([...old, ...value.events].map((e) => [e.id, e])).values(),
      ]);
      setOlder(
        value.has_more
          ? { after: value.next_after, through: older.through }
          : null,
      );
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }
  if (!study) return null;
  return (
    <section className="research-history" aria-label="История исследования">
      <form
        className="runtime-form"
        aria-label="Сравнение версий исследования"
        onSubmit={async (e) => {
          e.preventDefault();
          setError("");
          try {
            setDiff(
              await live.get(
                `/studies/${study}/diff?${new URLSearchParams({ from: String(from), to: String(to), ...(target ? { target } : {}) })}`,
              ),
            );
          } catch (err) {
            setError(err.message);
          }
        }}
      >
        <label>
          Исходный снимок
          <input
            type="number"
            min="1"
            max={revision}
            required
            value={from}
            onChange={(e) => setFrom(Number(e.target.value))}
          />
        </label>
        <label>
          Конечный снимок
          <input
            type="number"
            min={from}
            max={revision}
            required
            value={to}
            onChange={(e) => setTo(Number(e.target.value))}
          />
        </label>
        <label className="wide">
          Утверждение
          <select value={target} onChange={(e) => setTarget(e.target.value)}>
            <option value="">Все утверждения исследования</option>
            {entities.map((e) => (
              <option key={e.id} value={e.id}>
                {e.title}
              </option>
            ))}
          </select>
        </label>
        <button className="button secondary" disabled={loading}>
          <GitCompareArrows size={15} />
          Сравнить версии
        </button>
      </form>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {diff && (
        <section className="revision-diff" aria-label="Различия версий">
          <h3>
            Снимки {diff.from} и {diff.to}
          </h3>
          {!diff.entities.length && !diff.checks.length && (
            <p>Изменений не найдено.</p>
          )}
          {diff.entities.map((e) => (
            <details key={e.id}>
              <summary>
                {e.title} · версия {e.before_revision || "отсутствует"} /{" "}
                {e.after_revision || "отсутствует"}
              </summary>
              {e.changes.map((change) => (
                <div className="diff-field" key={change.field}>
                  <h4>{labels[change.field] || change.field}</h4>
                  <div className="diff-values">
                    <div>
                      <strong>Было</strong>
                      <pre>{show(change.before)}</pre>
                    </div>
                    <div>
                      <strong>Стало</strong>
                      <pre>{show(change.after)}</pre>
                    </div>
                  </div>
                </div>
              ))}
            </details>
          ))}
          {diff.checks.map((c) => (
            <div className="verification-row" key={c.id}>
              <code>{c.id}</code>
              <p>
                Актуальность проверки:{" "}
                {c.before_current ? "действующая" : "не действующая"} /{" "}
                {c.after_current ? "действующая" : "не действующая"}.
              </p>
              <code>{c.source_sha256}</code>
            </div>
          ))}
        </section>
      )}
      <div className="section-intro">
        <h3>Хронология исследования</h3>
        <span>Событий загружено: {events.length}</span>
      </div>
      <div className="timeline">
        {[...events]
          .sort((a, b) => b.id - a.id)
          .map((e) => (
            <article className="timeline-entry" key={e.id}>
              <span className="timeline-point" />
              <div className="event-meta">
                <time>{e.time}</time>
                <span>{e.actor}</span>
                <code>v{e.id}</code>
              </div>
              <h3>{e.label}</h3>
              <p>{e.detail}</p>
              <button className="button ghost" onClick={() => onOpen(e)}>
                <History size={14} />
                Открыть снимок
              </button>
            </article>
          ))}
      </div>
      {older && (
        <button
          className="button secondary"
          disabled={loading}
          onClick={earlier}
        >
          <ChevronDown size={15} />
          Ранние события
        </button>
      )}
    </section>
  );
}
