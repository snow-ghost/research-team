import React, { useEffect, useState } from "react";
import { Plus, Save, Play, Search, Trash2, GitBranch } from "lucide-react";
import { api } from "./api.js";

const roles = {
  proof: "Доказательство",
  counterexample: "Контрпримеры",
  formalize: "Формализация",
  review: "Рецензия",
};
const limitNames = {
  timeout_seconds: "Время попытки, с",
  max_steps: "Запросы модели",
  max_tool_calls: "Вызовы инструментов",
  max_output_tokens: "Токены на ответ",
  max_output_bytes: "Объем ответа, байт",
};
const lines = (s) =>
  s
    .split("\n")
    .map((v) => v.trim())
    .filter(Boolean);

export function ProfileFactory({ live }) {
  const [templates, setTemplates] = useState({}),
    [template, setTemplate] = useState(""),
    [error, setError] = useState(""),
    [open, setOpen] = useState(false),
    [skills, setSkills] = useState([]);
  useEffect(() => {
    let active = true;
    api("/profile-templates")
      .then((v) => {
        if (active) {
          setTemplates(v);
          setTemplate(Object.keys(v)[0] || "");
        }
      })
      .catch((e) => active && setError(e.message));
    return () => {
      active = false;
    };
  }, []);
  const p = templates[template];
  function choose(id) {
    setTemplate(id);
    setSkills(templates[id]?.skills || []);
  }
  return (
    <section className="process-section" aria-label="Фабрика агентов">
      <div className="section-intro">
        <h2>Профили агентов</h2>
        <button
          className="button secondary"
          disabled={live.busy || !p}
          onClick={() => {
            choose(template);
            setOpen(!open);
          }}
        >
          <Plus size={15} />
          Создать версию
        </button>
      </div>
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
      {open && p && (
        <form
          className="runtime-form"
          key={template}
          aria-label="Создание профиля"
          onSubmit={async (e) => {
            e.preventDefault();
            const f = Object.fromEntries(new FormData(e.currentTarget));
            setError("");
            try {
              await live.command("/profiles", {
                name: f.name,
                label: f.label,
                template,
                model: f.model,
                skills,
                tools: p.model
                  ? ["check_lean", "check_refutation", "read_file"].filter(
                      (n) => f[n] === "on",
                    )
                  : [],
                limits: Object.fromEntries(
                  Object.keys(limitNames).map((n) => [n, Number(f[n])]),
                ),
                confirm: f.confirm === "on",
              });
              setOpen(false);
            } catch (err) {
              setError(err.message);
            }
          }}
        >
          <label>
            Шаблон
            <select value={template} onChange={(e) => choose(e.target.value)}>
              {Object.keys(templates).map((id) => (
                <option key={id}>{id}</option>
              ))}
            </select>
          </label>
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
          <label>
            Модель
            <input
              name="model"
              required
              defaultValue={p.model?.model || p.external?.model}
            />
          </label>
          <dl className="wide">
            <dt>Адрес модели</dt>
            <dd>
              {p.model?.base_url ||
                p.external?.base_url ||
                "Среда внешнего исполнителя"}
            </dd>
            <dt>Источник ключа</dt>
            <dd>
              {p.model?.token_env ||
                Object.values(p.external?.secret_env || {}).join(", ") ||
                "Без ключа"}
            </dd>
          </dl>
          {Object.entries(limitNames).map(([n, label]) => (
            <label key={n}>
              {label}
              <input
                name={n}
                type="number"
                min={
                  n === "max_tool_calls"
                    ? 0
                    : n === "max_output_bytes"
                      ? 1024
                      : p.external && p.external.provider !== "coddy-agent"
                        ? 0
                        : 1
                }
                required
                defaultValue={p.limits[n]}
              />
            </label>
          ))}
          {p.model && (
            <fieldset className="wide">
              <legend>Инструменты</legend>
              {["check_lean", "check_refutation", "read_file"].map((n) => (
                <label className="checkbox-label" key={n}>
                  <input
                    type="checkbox"
                    name={n}
                    defaultChecked={p.model.tools?.includes(n)}
                  />
                  {n}
                </label>
              ))}
            </fieldset>
          )}
          <fieldset className="wide">
            <legend>Навыки</legend>
            {skills.map((skill, i) => (
              <div className="skill-editor" key={i}>
                <label>
                  Имя
                  <input
                    required
                    value={skill.id}
                    onChange={(e) =>
                      setSkills(
                        skills.map((v, j) =>
                          j === i ? { ...v, id: e.target.value } : v,
                        ),
                      )
                    }
                  />
                </label>
                <label>
                  Версия
                  <input
                    required
                    value={skill.version}
                    onChange={(e) =>
                      setSkills(
                        skills.map((v, j) =>
                          j === i ? { ...v, version: e.target.value } : v,
                        ),
                      )
                    }
                  />
                </label>
                <label className="wide">
                  Инструкции
                  <textarea
                    required
                    value={skill.instructions}
                    onChange={(e) =>
                      setSkills(
                        skills.map((v, j) =>
                          j === i ? { ...v, instructions: e.target.value } : v,
                        ),
                      )
                    }
                  />
                </label>
                <button
                  type="button"
                  className="icon-button"
                  title="Удалить навык"
                  aria-label="Удалить навык"
                  onClick={() => setSkills(skills.filter((_, j) => j !== i))}
                >
                  <Trash2 size={16} />
                </button>
              </div>
            ))}
            <button
              type="button"
              className="button secondary"
              disabled={skills.length >= 16}
              onClick={() =>
                setSkills([
                  ...skills,
                  { id: "", version: "1", instructions: "" },
                ])
              }
            >
              <Plus size={15} />
              Добавить навык
            </button>
          </fieldset>
          <label className="checkbox-label wide">
            <input type="checkbox" name="confirm" required />
            Подтверждаю модель, навыки и ограничения
          </label>
          <button className="button primary" disabled={live.busy}>
            <Save size={15} />
            Создать версию
          </button>
        </form>
      )}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Версия</th>
              <th>Шаблон</th>
              <th>Дата</th>
            </tr>
          </thead>
          <tbody>
            {(live.data.state.profile_revisions || []).map((r) => (
              <tr key={r.id}>
                <td>
                  {r.label} · {r.id}
                </td>
                <td>{r.template}</td>
                <td>{new Date(r.created_at).toLocaleString("ru-RU")}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

export function ComparisonPanel({ live }) {
  const statuses = {
    verified_not_accepted: "Проверено, без приемки",
    requires_review: "Требует рецензии",
    failed: "Ошибка",
    inconclusive: "Вывод не получен",
  };
  return (
    <section className="process-section" aria-label="Сравнение исполнителей">
      <h2>Сравнительные испытания</h2>
      {(live.data.state.comparisons || []).map((entry) => (
        <div key={entry.id}>
          <p>
            {new Date(entry.created_at).toLocaleString("ru-RU")} ·{" "}
            {entry.report.runs.length} запусков
          </p>
          {entry.report.runs.some(
            (run) =>
              run.verification?.status === "verified" &&
              !run.verification.audit_sha256,
          ) && (
            <p role="alert">
              Проверки Lean выполнены прежним механизмом без контроля
              происхождения цели. Результаты требуют повторного аудита; приемка
              не выполнена.
            </p>
          )}
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Задача</th>
                  <th>Состав</th>
                  <th>Результат</th>
                  <th>Запросы, измерено</th>
                  <th>Запросы, верхняя оценка</th>
                  <th>Входные токены</th>
                  <th>Выходные токены</th>
                  <th>Время, с</th>
                </tr>
              </thead>
              <tbody>
                {entry.report.runs.map((r) => (
                  <tr key={`${r.case}-${r.mode}`}>
                    <td>{r.case}</td>
                    <td>
                      {r.mode === "single" ? "Один исследователь" : "Команда"}
                    </td>
                    <td>{statuses[r.status] || r.status}</td>
                    <td>
                      {r.unknown_request_count
                        ? "Не полностью измерено"
                        : r.measured_requests}
                    </td>
                    <td>{r.request_upper_bound}</td>
                    <td>
                      {r.usage_incomplete
                        ? "Не полностью измерено"
                        : r.input_tokens}
                    </td>
                    <td>
                      {r.usage_incomplete
                        ? "Не полностью измерено"
                        : r.output_tokens}
                    </td>
                    <td>
                      {r.time_incomplete ? "Не измерено" : r.seconds.toFixed(1)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ))}
    </section>
  );
}

function TeamFields({ live }) {
  return (
    <>
      {Object.entries(roles).map(([role, label]) => (
        <label key={role}>
          {label}
          <select
            name={role}
            required
            defaultValue={
              live.data.profiles.find((p) => p.available && p.id.includes(role))
                ?.id || ""
            }
          >
            <option value="" disabled>
              Выберите профиль
            </option>
            {live.data.profiles.map((p) => (
              <option key={p.id} value={p.id} disabled={!p.available}>
                {p.label} · {p.id}
              </option>
            ))}
          </select>
        </label>
      ))}
      <label>
        Каталог
        <select name="workspace" required>
          {live.data.workspaces.map((w) => (
            <option key={w.id} value={w.id}>
              {w.label}
            </option>
          ))}
        </select>
      </label>
      <label>
        Попытки на ветвь
        <input
          name="max_attempts"
          type="number"
          min="4"
          max="20"
          defaultValue="6"
          required
        />
      </label>
    </>
  );
}

export function PlanningPanel({ live, study }) {
  const [open, setOpen] = useState(false),
    [error, setError] = useState(""),
    [apply, setApply] = useState("");
  const state = live.data.state,
    proposals = (state.proposals || []).filter(
      (p) => state.entities.find((e) => e.id === p.target)?.study === study,
    );
  const attempts = state.attempts.filter(
    (a) =>
      a.status === "candidate" &&
      state.tasks.find((t) => t.id === a.task_id)?.kind === "decompose" &&
      state.entities.find((e) => e.id === a.target)?.study === study &&
      !proposals.some((p) => p.attempt === a.id),
  );
  async function act(fn) {
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(e.message);
    }
  }
  return (
    <section className="process-section" aria-label="Планы исследования">
      <div className="section-intro">
        <h2>Планы исследования</h2>
        <button
          className="button secondary"
          disabled={live.busy || state.paused}
          onClick={() => setOpen(!open)}
        >
          <Plus size={15} />
          Запросить план
        </button>
      </div>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {open && (
        <form
          className="runtime-form"
          aria-label="Запрос плана"
          onSubmit={(e) => {
            e.preventDefault();
            const f = Object.fromEntries(new FormData(e.currentTarget));
            act(async () => {
              await live.command("/planning", {
                ...f,
                confirm: f.confirm === "on",
              });
              setOpen(false);
            });
          }}
        >
          <label>
            Цель
            <select name="target" required>
              {state.entities
                .filter(
                  (e) =>
                    e.study === study &&
                    e.formal_goal &&
                    !["accepted", "refuted"].includes(e.status),
                )
                .map((e) => (
                  <option key={e.id} value={e.id}>
                    {e.title}
                  </option>
                ))}
            </select>
          </label>
          <label>
            Планировщик
            <select name="profile" required>
              {live.data.profiles.map((p) => (
                <option key={p.id} value={p.id} disabled={!p.available}>
                  {p.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            Каталог
            <select name="workspace" required>
              {live.data.workspaces.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.label}
                </option>
              ))}
            </select>
          </label>
          <label className="checkbox-label wide">
            <input name="confirm" type="checkbox" required />
            Подтверждаю запрос плана и расходы
          </label>
          <button className="button primary" disabled={live.busy}>
            <Play size={15} />
            Запросить
          </button>
        </form>
      )}
      {attempts.map((a) => (
        <div className="team-row" key={a.id}>
          <span>
            {a.profile} · {a.id}
          </span>
          <button
            className="button secondary"
            disabled={live.busy}
            onClick={() =>
              act(() => live.command("/decompositions", { attempt: a.id }))
            }
          >
            <Save size={15} />
            Записать предложение
          </button>
        </div>
      ))}
      {proposals.map((p) => (
        <div className="team-row" key={p.id}>
          <div className="section-intro">
            <strong>
              {state.entities.find((e) => e.id === p.target)?.title}
            </strong>
            <span>
              {{
                applied: "Подтвержден",
                proposed: "Предложен",
                stale: "Устарел",
              }[p.status] || p.status}
            </span>
          </div>
          <p>{p.report.summary}</p>
          {[...p.report.claims, p.report.coverage].map((c, i) => (
            <details key={i}>
              <summary>
                {i === p.report.claims.length ? "Покрытие: " : "Лемма: "}
                {c.title}
              </summary>
              <p>{c.statement}</p>
              <p>{c.assumptions}</p>
              <p>
                Основания:{" "}
                {(c.depends_on || [])
                  .map((n) => p.report.claims[n]?.title)
                  .join(", ") || "Нет"}
              </p>
              {c.formal_goal && <pre>{c.formal_goal.source}</pre>}
            </details>
          ))}
          {(p.report.strategies || []).map((s, i) => (
            <p key={i}>
              {s.target === -1
                ? "Исходная цель"
                : s.target === p.report.claims.length
                  ? p.report.coverage.title
                  : p.report.claims[s.target]?.title}{" "}
              · {s.method} · Приоритет {s.priority} · {s.rationale}
            </p>
          ))}
          {p.status === "proposed" && (
            <button
              className="button secondary"
              disabled={live.busy}
              onClick={() => setApply(apply === p.id ? "" : p.id)}
            >
              <GitBranch size={15} />
              Проверить и подтвердить
            </button>
          )}
          {apply === p.id && (
            <form
              className="runtime-form"
              aria-label="Подтверждение плана"
              onSubmit={(e) => {
                e.preventDefault();
                const f = Object.fromEntries(new FormData(e.currentTarget));
                act(async () => {
                  await live.command(`/decompositions/${p.id}/apply`, {
                    confirm: f.confirm === "on",
                    team: p.report.strategies?.length
                      ? {
                          profiles: Object.fromEntries(
                            Object.keys(roles).map((r) => [r, f[r]]),
                          ),
                          workspace: f.workspace,
                          max_attempts: Number(f.max_attempts),
                          require_lean: true,
                        }
                      : undefined,
                  });
                  setApply("");
                });
              }}
            >
              {!!p.report.strategies?.length && <TeamFields live={live} />}
              <label className="checkbox-label wide">
                <input type="checkbox" name="confirm" required />
                Подтверждаю предпосылки, покрытие, цели и расходы ветвей
              </label>
              <button className="button primary" disabled={live.busy}>
                <Save size={15} />
                Подтвердить план
              </button>
            </form>
          )}
        </div>
      ))}
    </section>
  );
}

export function StructuredLemmaSearch({ live, target }) {
  const [error, setError] = useState(""),
    [hits, setHits] = useState([]),
    [index, setIndex] = useState(false),
    [searched, setSearched] = useState(false),
    [parameters, setParameters] = useState([{ name: "n", type: "Nat" }]);
  const state = live.data.state;
  async function act(fn) {
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(e.message);
    }
  }
  return (
    <section
      className="process-section"
      aria-label="Структурированный поиск лемм"
    >
      <div className="section-intro">
        <h2>Структура лемм</h2>
        <button className="button secondary" onClick={() => setIndex(!index)}>
          <Plus size={15} />
          Описать лемму
        </button>
      </div>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {index && (
        <form
          className="runtime-form"
          aria-label="Описание структуры леммы"
          onSubmit={(e) => {
            e.preventDefault();
            const f = Object.fromEntries(new FormData(e.currentTarget));
            act(async () => {
              await live.command("/lemmas/signatures", {
                signature: {
                  library: f.library,
                  domain: f.domain,
                  parameters,
                  conditions: lines(f.conditions),
                  conclusion: f.conclusion,
                },
              });
              setIndex(false);
            });
          }}
        >
          <label>
            Лемма
            <select name="library" required>
              {(state.library || [])
                .filter(
                  (l) =>
                    l.status === "ready" &&
                    state.entities.find((e) => e.id === l.lemma)?.status ===
                      "accepted",
                )
                .map((l) => (
                  <option key={l.id} value={l.id}>
                    {state.entities.find((e) => e.id === l.lemma)?.title}
                  </option>
                ))}
            </select>
          </label>
          <label>
            Область
            <input name="domain" required />
          </label>
          <fieldset className="wide">
            <legend>Параметры</legend>
            {parameters.map((p, i) => (
              <div className="skill-editor" key={i}>
                <label>
                  Имя параметра
                  <input
                    required
                    value={p.name}
                    onChange={(e) =>
                      setParameters(
                        parameters.map((v, j) =>
                          j === i ? { ...v, name: e.target.value } : v,
                        ),
                      )
                    }
                  />
                </label>
                <label>
                  Тип параметра
                  <input
                    required
                    value={p.type}
                    onChange={(e) =>
                      setParameters(
                        parameters.map((v, j) =>
                          j === i ? { ...v, type: e.target.value } : v,
                        ),
                      )
                    }
                  />
                </label>
                <button
                  type="button"
                  className="icon-button"
                  aria-label="Удалить параметр"
                  title="Удалить параметр"
                  onClick={() =>
                    setParameters(parameters.filter((_, j) => j !== i))
                  }
                >
                  <Trash2 size={15} />
                </button>
              </div>
            ))}
            <button
              type="button"
              className="button secondary"
              disabled={parameters.length >= 16}
              onClick={() =>
                setParameters([...parameters, { name: "", type: "" }])
              }
            >
              <Plus size={15} />
              Добавить параметр
            </button>
          </fieldset>
          <label className="wide">
            Условия
            <textarea name="conditions" />
          </label>
          <label className="wide">
            Вид заключения
            <input name="conclusion" required />
          </label>
          <button className="button primary" disabled={live.busy}>
            <Save size={15} />
            Записать описание
          </button>
        </form>
      )}
      <form
        className="runtime-form"
        aria-label="Поиск по структуре"
        onSubmit={(e) => {
          e.preventDefault();
          const f = Object.fromEntries(new FormData(e.currentTarget));
          act(async () => {
            const v = await live.command("/lemmas/search", {
              domain: f.domain,
              conclusion: f.conclusion,
              parameter_types: lines(f.types),
              conditions: lines(f.conditions),
            });
            setHits(v.hits);
            setSearched(true);
          });
        }}
      >
        <label>
          Область
          <input name="domain" />
        </label>
        <label>
          Вид заключения
          <input name="conclusion" />
        </label>
        <label>
          Типы параметров
          <textarea name="types" />
        </label>
        <label>
          Доступные условия
          <textarea name="conditions" />
        </label>
        <button className="button secondary" disabled={live.busy}>
          <Search size={15} />
          Найти
        </button>
      </form>
      {searched && !hits.length && <p role="status">Леммы не найдены.</p>}
      {hits.map((h) => (
        <div className="team-row" key={h.signature.id}>
          <strong>
            {state.entities.find((e) => e.id === h.lemma)?.title || h.lemma}
          </strong>
          <p>{h.candidate}</p>
          <p>
            Условия для проверки:{" "}
            {h.missing_conditions.join("; ") || "Явные условия совпали"}
          </p>
          <form
            className="runtime-form"
            onSubmit={(e) => {
              e.preventDefault();
              const f = Object.fromEntries(new FormData(e.currentTarget));
              act(() =>
                live.command("/lemmas/applications", {
                  target: f.target,
                  library: h.signature.library,
                  substitution: f.substitution,
                  confirm: f.confirm === "on",
                }),
              );
            }}
          >
            <label>
              Цель
              <select name="target" required defaultValue={target}>
                {state.entities
                  .filter(
                    (e) =>
                      e.formal_goal &&
                      !["accepted", "refuted"].includes(e.status),
                  )
                  .map((e) => (
                    <option key={e.id} value={e.id}>
                      {e.title}
                    </option>
                  ))}
              </select>
            </label>
            <label>
              Подстановка
              <textarea name="substitution" required />
            </label>
            <label className="checkbox-label wide">
              <input type="checkbox" name="confirm" required />
              Подтверждаю создание обязательства Lean
            </label>
            <button className="button secondary" disabled={live.busy}>
              <Plus size={15} />
              Создать проверку переноса
            </button>
          </form>
        </div>
      ))}
    </section>
  );
}
