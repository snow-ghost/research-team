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
                  applicability: {
                    roles: [
                      "proof",
                      "counterexample",
                      "formalize",
                      "review",
                      "decompose",
                      "answer",
                    ].filter((r) => f[`role_${r}`] === "on"),
                    require_formal_goal: f.require_formal_goal === "on",
                    require_verified_candidate:
                      f.require_verified_candidate === "on",
                    require_accepted_dependencies:
                      f.require_accepted_dependencies === "on",
                    min_remaining_attempts: Number(f.min_remaining_attempts),
                    priority: Number(f.priority),
                    exclusive_group: f.exclusive_group,
                  },
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
            <input name="require_formal_goal" type="checkbox" />
            Закрепленная цель Lean
          </label>
          <label className="checkbox-label wide">
            <input name="require_verified_candidate" type="checkbox" />
            Проверенный кандидат
          </label>
          <label className="checkbox-label wide">
            <input name="require_accepted_dependencies" type="checkbox" />
            Принятые основания
          </label>
          <fieldset className="wide">
            <legend>Роли</legend>
            {Object.entries({
              proof: "Доказательство",
              counterexample: "Контрпримеры",
              formalize: "Формализация",
              review: "Рецензия",
              decompose: "План исследования",
              answer: "Ответ на вопрос",
            }).map(([r, label]) => (
              <label className="checkbox-label" key={r}>
                <input type="checkbox" name={`role_${r}`} />
                {label}
              </label>
            ))}
          </fieldset>
          <label>
            Приоритет выбора
            <input
              name="priority"
              type="number"
              min={0}
              max={100}
              defaultValue={0}
              required
            />
          </label>
          <label>
            Группа выбора
            <input
              name="exclusive_group"
              pattern="[a-z][a-z0-9-]{0,47}"
              maxLength={48}
            />
          </label>
          <label>
            Минимальный остаток попыток
            <input
              name="min_remaining_attempts"
              type="number"
              min="0"
              max="20"
              defaultValue="0"
              required
            />
          </label>
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
