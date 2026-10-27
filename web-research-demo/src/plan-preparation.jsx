import React, { useState } from "react";
import { Check, Code, ShieldCheck, X } from "lucide-react";

const states = {
  queued: "В очереди",
  running: "Проверка целей",
  ready: "Определения собраны",
  failed: "Ошибка сборки",
  interrupted: "Проверка прервана",
  stale: "Проверка устарела",
};

export function PlanPreparationPanel({ live, proposal }) {
  const [editing, setEditing] = useState(false),
    [error, setError] = useState("");
  const p = proposal,
    c = p.preparation,
    active = ["queued", "running"].includes(c?.status);
  async function act(fn) {
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(e.message);
    }
  }
  return (
    <section className="plan-preparation" aria-label={`Проверка плана ${p.id}`}>
      <div className="section-intro">
        <strong>
          {c ? states[c.status] || c.status : "Цели не проверены"}
        </strong>
        <span>{c?.report?.definitions?.length || 0} модулей определений</span>
      </div>
      {c?.diagnostics && <pre>{c.diagnostics}</pre>}
      {c?.report?.definitions?.map((d, i) => (
        <details key={d.module}>
          <summary>
            {d.goal.declaration} ·{" "}
            {d.report.status === "ready" ? "Собран" : "Ошибка"}
          </summary>
          <p>{d.module}</p>
          <p>
            Сумма исходника: <code>{d.report.source_sha256}</code>
          </p>
          {d.report.diagnostics && <pre>{d.report.diagnostics}</pre>}
        </details>
      ))}
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {p.status === "proposed" && (
        <div className="action-row">
          <button
            className="button secondary"
            disabled={
              live.busy ||
              active ||
              c?.status === "ready" ||
              !live.data.lean.configured
            }
            onClick={() =>
              act(() =>
                live.command(`/decompositions/${p.id}/prepare`, {
                  note: "Местная проверка целей и совместных импортов перед утверждением плана",
                  confirm: true,
                }),
              )
            }
          >
            <ShieldCheck size={15} />
            Проверить цели
          </button>
          <button
            className="button secondary"
            disabled={live.busy || active}
            onClick={() => setEditing(!editing)}
          >
            <Code size={15} />
            Редактировать предложение
          </button>
        </div>
      )}
      {editing && (
        <form
          className="runtime-form"
          aria-label="Редакция предложения плана"
          onSubmit={(e) => {
            e.preventDefault();
            const f = Object.fromEntries(new FormData(e.currentTarget));
            act(async () => {
              const report = JSON.parse(f.report);
              await live.command(`/decompositions/${p.id}/revisions`, {
                report,
                note: f.note,
                confirm: f.confirm === "on",
              });
              setEditing(false);
            });
          }}
        >
          <label className="wide">
            План JSON
            <textarea
              name="report"
              required
              defaultValue={JSON.stringify(p.report, null, 2)}
              rows={16}
            />
          </label>
          <label className="wide">
            Основание редакции
            <textarea name="note" required maxLength={4000} />
          </label>
          <label className="checkbox-label wide">
            <input name="confirm" type="checkbox" required />
            Подтверждаю отдельную редакцию с сохранением исходного ответа
          </label>
          <button className="button primary" disabled={live.busy}>
            <Check size={15} />
            Сохранить редакцию
          </button>
          <button
            className="button secondary"
            type="button"
            onClick={() => setEditing(false)}
          >
            <X size={15} />
            Отмена
          </button>
        </form>
      )}
    </section>
  );
}
