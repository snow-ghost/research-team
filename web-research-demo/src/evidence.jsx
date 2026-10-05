import React, { useEffect, useState } from "react";
import { Link, BookOpen, GitBranch, Download, Check, Plus } from "lucide-react";

const labels = {
  ready: "Проверки связаны",
  accepted: "Принято",
  stale: "Устарело",
  needs_changes: "Нужны исправления",
  queued: "В очереди",
  running: "Сборка",
  failed: "Ошибка",
  interrupted: "Прервано",
  proposed: "Предложено",
  applied: "Применено",
};
const download = (entry) => {
  const url = URL.createObjectURL(
    new Blob([entry.source], { type: "text/plain;charset=utf-8" }),
  );
  const a = document.createElement("a");
  a.href = url;
  a.download = entry.module + ".lean";
  a.click();
  URL.revokeObjectURL(url);
};
export function EvidencePanel({ live, study, selected }) {
  const { state } = live.data;
  const [error, setError] = useState(""),
    [open, setOpen] = useState(false),
    [observation, setObservation] = useState(null);
  const target =
    state.entities.find((e) => e.id === selected && e.study === study) ||
    state.entities.find(
      (e) => e.id === state.studies.find((s) => s.id === study)?.goal,
    );
  const belongs = (id) =>
    state.entities.find((e) => e.id === id)?.study === study;
  useEffect(() => {
    let cancelled = false;
    const poll = async () => {
      try {
        const value = await live.get("/studies/" + study + "/observability");
        if (!cancelled) setObservation(value);
      } catch (e) {
        if (!cancelled) setError(e.message);
      }
    };
    setObservation(null);
    poll();
    const timer = setInterval(poll, 2000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [study, live.get]);
  async function command(route, body) {
    setError("");
    try {
      await live.command(route, body);
    } catch (e) {
      setError(e.message);
    }
  }
  const results = (state.results || []).filter((r) => belongs(r.target));
  const proposals = (state.proposals || []).filter((p) => belongs(p.target));
  const library = (state.library || []).filter((l) => belongs(l.lemma));
  const candidates = state.attempts.filter(
    (a) => belongs(a.target) && a.status === "candidate",
  );
  const role = (a, kind) =>
    state.tasks.find((t) => t.id === a.task_id)?.kind === kind;
  return (
    <section
      className="process-section evidence-panel"
      aria-label="Проверяемые результаты"
    >
      <div className="section-intro">
        <h2>Результаты и основания</h2>
        <button
          className="button secondary"
          disabled={live.busy || !target?.proof}
          onClick={() => setOpen(!open)}
        >
          <Link size={15} />
          Связать проверки
        </button>
      </div>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {observation && (
        <dl className="evidence-metrics">
          <div>
            <dt>Попытки</dt>
            <dd>
              {observation.completed}/{observation.attempts}
            </dd>
          </div>
          <div>
            <dt>В работе</dt>
            <dd>{observation.active}</dd>
          </div>
          <div>
            <dt>Входные токены</dt>
            <dd>{observation.input_tokens}</dd>
          </div>
          <div>
            <dt>Выходные токены</dt>
            <dd>{observation.output_tokens}</dd>
          </div>
          <div>
            <dt>Расход неизвестен</dt>
            <dd>{observation.unknown_usage}</dd>
          </div>
          <div>
            <dt>Открытые вопросы</dt>
            <dd>{observation.pending_questions.length}</dd>
          </div>
        </dl>
      )}
      {open && (
        <form
          className="runtime-form"
          aria-label="Связь проверок"
          onSubmit={async (e) => {
            e.preventDefault();
            const v = Object.fromEntries(new FormData(e.currentTarget));
            await command("/results", {
              target: target.id,
              review_attempt: v.review,
              counter_attempt: v.counter,
              audit_legacy_input: v.legacy === "on",
            });
          }}
        >
          <label>
            Рецензия
            <select name="review" required>
              {candidates
                .filter((a) => a.target === target.id && role(a, "review"))
                .map((a) => (
                  <option value={a.id} key={a.id}>
                    {a.id}
                  </option>
                ))}
            </select>
          </label>
          <label>
            Контрпримеры
            <select name="counter" required>
              {candidates
                .filter(
                  (a) => a.target === target.id && role(a, "counterexample"),
                )
                .map((a) => (
                  <option value={a.id} key={a.id}>
                    {a.id}
                  </option>
                ))}
            </select>
          </label>
          <label className="checkbox wide">
            <input name="legacy" type="checkbox" />
            Проверить точный файл и суммы в исходном задании старой рецензии
          </label>
          <button className="button primary" disabled={live.busy}>
            <Link size={15} />
            Сохранить связь
          </button>
        </form>
      )}
      {results.map((r) => (
        <details className="verification-row" key={r.id}>
          <summary>
            {labels[r.status]} ·{" "}
            {state.entities.find((e) => e.id === r.target)?.title} · версия{" "}
            {r.target_revision}
          </summary>
          <p>
            Автор: {r.author}
            <br />
            Рецензия: {r.review_attempt}
            <br />
            Контрпримеры: {r.counter_attempt}
          </p>
          <p>{r.review.summary}</p>
          <small>
            Цель: {r.binding.goal_sha256}
            <br />
            Файл: {r.binding.source_sha256}
            <br />
            Среда: {r.environment_sha256}
          </small>
          {r.review.findings.map((f, i) => (
            <p key={i}>
              {f.severity}: {f.text}
            </p>
          ))}
          <p>{r.counter.evidence}</p>
          {r.status === "accepted" &&
            !library.some(
              (l) =>
                l.lemma === r.target &&
                ["ready", "queued", "running"].includes(l.status),
            ) && (
              <button
                className="button secondary"
                disabled={live.busy}
                onClick={() => command("/library", { lemma: r.target })}
              >
                <BookOpen size={15} />
                Собрать модуль Lean
              </button>
            )}
        </details>
      ))}
      <div className="section-intro">
        <h3>Разбиения</h3>
        <button
          className="button secondary"
          disabled={
            live.busy ||
            !target ||
            ["accepted", "refuted"].includes(target.status)
          }
          onClick={() =>
            command("/actions", {
              type: "TASK",
              target: target.id,
              kind: "decompose",
              title: "Разбиение: " + target.title,
              text: 'Предложи разбиение точной цели на от одной до шести лемм и обязательство покрытия. Верни JSON {"summary":"...","claims":[{"title":"...","statement":"...","assumptions":"..."}],"coverage":{"title":"...","statement":"...","assumptions":"..."}}. Не объявляй части доказанными. Формальные цели можно указать полем formal_goal только при точном соответствии.',
            })
          }
        >
          <GitBranch size={15} />
          Задание разбиения
        </button>
      </div>
      {candidates
        .filter(
          (a) =>
            role(a, "decompose") && !proposals.some((p) => p.attempt === a.id),
        )
        .map((a) => (
          <button
            className="button secondary"
            key={a.id}
            disabled={live.busy}
            onClick={() => command("/decompositions", { attempt: a.id })}
          >
            <Plus size={15} />
            Сохранить предложение {a.id}
          </button>
        ))}
      {proposals.map((p) => (
        <details className="verification-row" key={p.id}>
          <summary>
            {labels[p.status]} · версия {p.target_revision}
          </summary>
          <p>{p.report.summary}</p>
          {p.report.claims.map((c, i) => (
            <div key={i}>
              <strong>{c.title}</strong>
              <p>{c.statement}</p>
              <small>{c.assumptions}</small>
            </div>
          ))}
          <strong>Покрытие: {p.report.coverage.title}</strong>
          <p>{p.report.coverage.statement}</p>
          {p.status === "proposed" && (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                command("/decompositions/" + p.id + "/apply", {
                  confirm: true,
                });
              }}
            >
              <label className="checkbox">
                <input type="checkbox" required />
                Подтверждаю части, предпосылки и формальные цели
              </label>
              <button className="button secondary" disabled={live.busy}>
                <Check size={15} />
                Применить разбиение
              </button>
            </form>
          )}
        </details>
      ))}
      <h3>Модули библиотеки</h3>
      {library.map((l) => (
        <details className="verification-row" key={l.id}>
          <summary>
            {labels[l.status] || l.status} ·{" "}
            {state.entities.find((e) => e.id === l.lemma)?.title}
          </summary>
          <code>{l.module}</code>
          <p>
            Версия: {l.lemma_revision} · результат: {l.result}
          </p>
          <pre>{l.report?.diagnostics || ""}</pre>
          <small>Модуль: {l.report?.artifact_sha256}</small>
          {l.status === "ready" && (
            <button className="button secondary" onClick={() => download(l)}>
              <Download size={15} />
              Скачать исходный модуль
            </button>
          )}
        </details>
      ))}
    </section>
  );
}
