import React, { useEffect, useState } from "react";
import { GitCompareArrows, Plus } from "lucide-react";

export function MemoryRelations({ live, target }) {
  const [rows, setRows] = useState([]),
    [open, setOpen] = useState(null),
    [error, setError] = useState("");
  useEffect(() => setOpen(null), [target]);
  useEffect(() => {
    let active = true;
    if (!target) {
      setRows([]);
      return;
    }
    live
      .get(`/memory/suggestions?target=${target}`)
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
  }, [target, live.data.state.revision, live.get]);
  if (!rows.length && !error) return null;
  return (
    <div className="memory-relations" aria-label="Связи памяти">
      <h3>Записи родственных постановок</h3>
      {rows.map((row) => (
        <article className="relation-row" key={row.memory.id}>
          <p>{row.memory.content}</p>
          <p>
            Различия:{" "}
            {row.differences
              .map(
                (x) =>
                  ({
                    statement: "утверждение",
                    assumptions: "условия",
                    formal_goal: "формальная цель",
                    required_libraries: "обязательные леммы",
                  })[x] || x,
              )
              .join(", ") || "разные версии"}
            .
          </p>
          <button
            className="button secondary"
            disabled={live.busy || row.status === "historical_only"}
            onClick={() =>
              setOpen(open === row.memory.id ? null : row.memory.id)
            }
          >
            <GitCompareArrows size={15} />
            Проверить связь условий
          </button>
          {row.relations.map((r) => (
            <p key={r.id}>
              Обязательство <code>{r.obligation}</code>:{" "}
              {{
                open: "открыто",
                stale: "устарело",
                formal_relation_verified: "формальная связь проверена",
              }[r.status] || r.status}
              .
            </p>
          ))}
          {open === row.memory.id && (
            <form
              className="runtime-form"
              aria-label="Обязательство связи памяти"
              onSubmit={async (e) => {
                e.preventDefault();
                const f = Object.fromEntries(new FormData(e.currentTarget));
                setError("");
                try {
                  await live.command("/memory/relations", {
                    memory: row.memory.id,
                    target,
                    goal: {
                      source: f.source,
                      declaration: f.declaration,
                      candidate: f.candidate,
                    },
                    note: f.note,
                    confirm: true,
                  });
                  setOpen(null);
                } catch (err) {
                  setError(err.message);
                }
              }}
            >
              <label className="wide">
                Формальная связь
                <textarea name="source" required maxLength={65536} />
              </label>
              <label>
                Декларация
                <input name="declaration" required />
              </label>
              <label>
                Кандидат
                <input name="candidate" required />
              </label>
              <label className="wide">
                Основание
                <textarea name="note" required maxLength={4000} />
              </label>
              <label className="checkbox wide">
                <input type="checkbox" required />
                Подтверждаю отдельное обязательство без автоматического переноса
                записи
              </label>
              <button className="button secondary" disabled={live.busy}>
                <Plus size={15} />
                Создать обязательство связи
              </button>
            </form>
          )}
        </article>
      ))}
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
    </div>
  );
}
