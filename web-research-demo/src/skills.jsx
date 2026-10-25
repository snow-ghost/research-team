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

const resultChecks = {
  lean_source: "Исходник Lean",
  counterexample_report: "Отчет контрпримеров",
  review_report: "Рецензия",
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
                  procedure: lines(f.procedure || ""),
                  result_checks: Object.keys(resultChecks).filter(
                    (c) => f[`check_${c}`] === "on",
                  ),
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
          <label className="wide">
            Порядок действий
            <textarea name="procedure" maxLength={16000} />
          </label>
          <fieldset className="wide">
            <legend>Проверки результата</legend>
            {Object.entries(resultChecks).map(([id, label]) => (
              <label className="checkbox-label" key={id}>
                <input type="checkbox" name={`check_${id}`} />
                {label}
              </label>
            ))}
          </fieldset>
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
      <SkillTrial live={live} />
    </section>
  );
}

function SkillTrial({ live }) {
  const [result, setResult] = useState(null),
    [error, setError] = useState("");
  const state = live.data.state;
  return (
    <section className="process-section" aria-label="Испытание навыка">
      <h3>Испытание навыка</h3>
      <form
        className="runtime-form"
        aria-label="Проверка навыка"
        onSubmit={async (e) => {
          e.preventDefault();
          setError("");
          const f = Object.fromEntries(new FormData(e.currentTarget));
          try {
            setResult(
              await live.command(`/skills/${f.skill}/trial`, {
                target: f.target,
                profile: f.profile,
                role: f.role,
                candidate: f.candidate,
              }),
            );
          } catch (err) {
            setError(err.message);
          }
        }}
      >
        <label>
          Навык
          <select name="skill" required>
            {(state.skill_revisions || []).map((s) => (
              <option key={s.id} value={s.id}>
                {s.label} · {s.id}
              </option>
            ))}
          </select>
        </label>
        <label>
          Профиль
          <select name="profile" required>
            {live.data.profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Утверждение
          <select name="target" required>
            {state.entities.map((e) => (
              <option key={e.id} value={e.id}>
                {e.title}
              </option>
            ))}
          </select>
        </label>
        <label>
          Роль
          <select name="role">
            <option value="proof">Доказательство</option>
            <option value="formalize">Формализация</option>
            <option value="counterexample">Контрпримеры</option>
            <option value="review">Рецензия</option>
            <option value="decompose">План</option>
            <option value="answer">Ответ</option>
          </select>
        </label>
        <label className="wide">
          Образец результата
          <textarea name="candidate" maxLength={65536} required />
        </label>
        <button
          className="button secondary"
          disabled={live.busy || !state.skill_revisions?.length}
        >
          <Save size={15} />
          Проверить образец
        </button>
      </form>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {result && (
        <div role="status">
          <p>
            {result.applicable
              ? "Условия применения выполнены"
              : "Условия применения не выполнены"}
          </p>
          {result.selections.map((s, i) => (
            <p key={i}>{s.reason}</p>
          ))}
          {result.checks.map((c, i) => (
            <p key={i}>
              {resultChecks[c.check] || c.check}:{" "}
              {c.passed ? "форма соответствует" : "ошибка формы"}
            </p>
          ))}
          <p>Математическая проверка не выполнена.</p>
        </div>
      )}
    </section>
  );
}
