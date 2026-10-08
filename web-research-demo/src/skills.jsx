import React, { useState } from "react";
import { Plus, Save } from "lucide-react";

const lines = (value) =>
  value
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
const fields = {
  preconditions: "Условия применения",
  inputs: "Входные данные",
  stop_criteria: "Критерии остановки",
  examples: "Примеры",
};

export function SkillRegistry({ live }) {
  const [open, setOpen] = useState(false),
    [error, setError] = useState("");
  return (
    <section className="process-section" aria-label="Каталог навыков">
      <div className="section-intro">
        <h2>Навыки</h2>
        <button
          className="button secondary"
          disabled={live.busy}
          onClick={() => setOpen(!open)}
        >
          <Plus size={15} />
          Создать версию навыка
        </button>
      </div>
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
      {open && (
        <form
          className="runtime-form"
          aria-label="Создание навыка"
          onSubmit={async (e) => {
            e.preventDefault();
            const f = Object.fromEntries(new FormData(e.currentTarget));
            setError("");
            try {
              await live.command("/skills", {
                name: f.name,
                label: f.label,
                instructions: f.instructions,
                contract: {
                  ...Object.fromEntries(
                    Object.keys(fields).map((key) => [key, lines(f[key])]),
                  ),
                  output: f.output,
                  required_tools: [
                    "read_file",
                    "check_lean",
                    "check_refutation",
                  ].filter((t) => f[t] === "on"),
                },
                confirm: f.confirm === "on",
              });
              setOpen(false);
            } catch (err) {
              setError(err.message);
            }
          }}
        >
          <label>
            Имя
            <input
              name="name"
              required
              pattern="[a-z][a-z0-9-]{0,47}"
              maxLength={48}
            />
          </label>
          <label>
            Название
            <input name="label" required maxLength={200} />
          </label>
          <label className="wide">
            Инструкции
            <textarea name="instructions" required maxLength={16000} />
          </label>
          {Object.entries(fields).map(([name, label]) => (
            <label key={name} className="wide">
              {label}
              <textarea
                name={name}
                required={name !== "examples"}
                maxLength={16000}
              />
            </label>
          ))}
          <label className="wide">
            Формат результата
            <textarea name="output" required maxLength={4000} />
          </label>
          <fieldset className="wide">
            <legend>Требуемые инструменты</legend>
            {["read_file", "check_lean", "check_refutation"].map((t) => (
              <label className="checkbox-label" key={t}>
                <input type="checkbox" name={t} />
                {t}
              </label>
            ))}
          </fieldset>
          <label className="checkbox-label wide">
            <input name="confirm" type="checkbox" required />
            Подтверждаю новую версию
          </label>
          <button className="button primary" disabled={live.busy}>
            <Save size={15} />
            Записать навык
          </button>
        </form>
      )}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Версия</th>
              <th>Результат</th>
              <th>Условия и процедура</th>
            </tr>
          </thead>
          <tbody>
            {(live.data.state.skill_revisions || []).map((r) => (
              <tr key={r.id}>
                <td>
                  {r.label} · {r.id}
                </td>
                <td>{r.configuration.contract?.output}</td>
                <td>
                  <details>
                    <summary>Сведения о навыке</summary>
                    <p>{r.configuration.instructions}</p>
                    {Object.entries(fields).map(([name, label]) => (
                      <div key={name}>
                        <strong>{label}</strong>
                        <ul>
                          {(r.configuration.contract?.[name] || []).map(
                            (v, i) => (
                              <li key={i}>{v}</li>
                            ),
                          )}
                        </ul>
                      </div>
                    ))}
                  </details>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
