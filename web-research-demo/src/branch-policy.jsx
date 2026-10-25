import React, { useEffect, useState } from "react";
import { Settings2 } from "lucide-react";

export function BranchPolicyForm({ live, branch, methods }) {
  const [choice, setChoice] = useState(null),
    [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    live
      .get(`/branches/recommendations?study=${branch.study}`)
      .then((rows) => {
        if (active) setChoice(rows.find((x) => x.branch === branch.id));
      })
      .catch((e) => {
        if (active) setError(e.message);
      });
    return () => {
      active = false;
    };
  }, [branch.id, live.data.state.revision, live.get]);
  const terminal = [
    "completed",
    "invalidated",
    "abandoned",
    "cancelled",
  ].includes(branch.status);
  return (
    <details className="branch-policy">
      <summary>Правила выбора ветви</summary>
      {choice && <p>{choice.reason}</p>}
      {branch.policy && (
        <p>
          Режим:{" "}
          {branch.policy.mode === "execute" ? "исполнение" : "наблюдение"}.
          Решений: {branch.policy.used} / {branch.policy.max_decisions}.
        </p>
      )}
      {!terminal && (
        <form
          className="runtime-form"
          aria-label={`Правила ветви ${branch.id}`}
          onSubmit={async (e) => {
            e.preventDefault();
            const f = new FormData(e.currentTarget);
            setError("");
            try {
              await live.command(`/branches/${branch.id}/policy`, {
                policy: {
                  enabled: f.has("enabled"),
                  mode: f.get("mode"),
                  methods: f.getAll("methods"),
                  max_decisions: Number(f.get("max_decisions")),
                  failure_threshold: Number(f.get("failure_threshold")),
                },
                confirm: true,
              });
            } catch (err) {
              setError(err.message);
            }
          }}
        >
          <label className="checkbox wide">
            <input
              name="enabled"
              type="checkbox"
              defaultChecked={branch.policy?.enabled || false}
            />
            Включить правила
          </label>
          <label>
            Режим
            <select name="mode" defaultValue={branch.policy?.mode || "observe"}>
              <option value="observe">Наблюдение</option>
              <option value="execute">Исполнение</option>
            </select>
          </label>
          <label>
            Предел решений
            <input
              name="max_decisions"
              type="number"
              min="1"
              max="5"
              defaultValue={branch.policy?.max_decisions || 2}
              required
            />
          </label>
          <label>
            Неуспешных попыток
            <input
              name="failure_threshold"
              type="number"
              min="1"
              max="5"
              defaultValue={branch.policy?.failure_threshold || 2}
              required
            />
          </label>
          <fieldset className="wide">
            <legend>Разрешенные приемы</legend>
            {methods.map((m) => (
              <label className="checkbox" key={m.id}>
                <input
                  name="methods"
                  value={m.id}
                  type="checkbox"
                  defaultChecked={
                    branch.policy?.methods.includes(m.id) ||
                    m.id === branch.method
                  }
                />
                {m.label}
              </label>
            ))}
          </fieldset>
          <label className="checkbox wide">
            <input type="checkbox" required />
            Подтверждаю предел решений и расходы в общем бюджете исследования
          </label>
          <button className="button secondary" disabled={live.busy}>
            <Settings2 size={15} />
            Сохранить правила
          </button>
        </form>
      )}
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
    </details>
  );
}
