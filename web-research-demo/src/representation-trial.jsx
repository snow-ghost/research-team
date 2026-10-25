import React, { useState } from "react";
import { GitCompareArrows } from "lucide-react";

export function RepresentationTrial({ live, target, libraries }) {
  const [error, setError] = useState("");
  const entity = live.data.state.entities.find((e) => e.id === target);
  if (!entity?.formal_goal) return null;
  return (
    <details className="representation-trial">
      <summary>Замена представления</summary>
      <form
        className="runtime-form"
        aria-label="Опыт смены представления"
        onSubmit={async (e) => {
          e.preventDefault();
          const f = Object.fromEntries(new FormData(e.currentTarget));
          setError("");
          try {
            await live.command("/experiments/representation", {
              target,
              library: f.library,
              direct: {
                source: entity.formal_goal.source,
                declaration: f.direct_declaration,
                candidate: f.direct_candidate,
              },
              bridge: {
                source: entity.formal_goal.source,
                declaration: f.bridge_declaration,
                candidate: f.bridge_candidate,
              },
              bridge_source: f.bridge_source,
              bridge_author: f.bridge_author,
              depth: Number(f.depth),
              timeout_seconds: Number(f.seconds),
              confirm: true,
            });
          } catch (err) {
            setError(err.message);
          }
        }}
      >
        <label className="wide">
          Принятая лемма
          <select name="library" defaultValue="" required>
            <option value="" disabled>
              Выберите модуль
            </option>
            {libraries.map((l) => (
              <option key={l.id} value={l.id}>
                {live.data.state.entities.find((e) => e.id === l.lemma)
                  ?.title || l.module}
              </option>
            ))}
          </select>
        </label>
        <label>
          Прямое утверждение
          <input name="direct_declaration" required />
        </label>
        <label>
          Кандидат прямого доказательства
          <input name="direct_candidate" required />
        </label>
        <label>
          Эквивалентность
          <input name="bridge_declaration" required />
        </label>
        <label>
          Кандидат эквивалентности
          <input name="bridge_candidate" required />
        </label>
        <label className="wide">
          Исходник эквивалентности
          <textarea name="bridge_source" required maxLength={64000} />
        </label>
        <label className="wide">
          Автор исходника
          <input name="bridge_author" required maxLength={300} />
        </label>
        <label>
          Глубина поиска
          <input
            name="depth"
            type="number"
            min="1"
            max="6"
            defaultValue="6"
            required
          />
        </label>
        <label>
          На проверку, с
          <input
            name="seconds"
            type="number"
            min="1"
            max="120"
            defaultValue="90"
            required
          />
        </label>
        <label className="checkbox wide">
          <input type="checkbox" required />
          Подтверждаю до четырех местных проверок без вызовов модели
        </label>
        <button
          className="button secondary"
          disabled={live.busy || !libraries.length}
        >
          <GitCompareArrows size={15} />
          Проверить замену представления
        </button>
      </form>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
    </details>
  );
}
