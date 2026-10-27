import React, { useEffect, useState } from "react";
import { ResearchMethodSelect } from "./assignment.jsx";
import {
  Plus,
  Save,
  Play,
  Search,
  Trash2,
  GitBranch,
  FlaskConical,
} from "lucide-react";
import { api } from "./api.js";
import { RepresentationTrial } from "./representation-trial.jsx";

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
    [skills, setSkills] = useState([]),
    [registry, setRegistry] = useState(false);
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
            const refs = new FormData(e.currentTarget).getAll("skill_ref");
            setError("");
            try {
              await live.command("/profiles", {
                name: f.name,
                label: f.label,
                template,
                model: f.model,
                skills: registry ? [] : skills,
                skill_refs: registry ? refs : [],
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
          <label className="checkbox-label wide">
            <input
              type="checkbox"
              checked={registry}
              onChange={(e) => setRegistry(e.target.checked)}
            />
            Версии из каталога навыков
          </label>
          {registry && (
            <fieldset className="wide">
              <legend>Закрепленные версии</legend>
              {(live.data.state.skill_revisions || []).map((r) => (
                <label className="checkbox-label" key={r.id}>
                  <input type="checkbox" name="skill_ref" value={r.id} />
                  {r.label} · {r.id}
                </label>
              ))}
            </fieldset>
          )}
          <fieldset className="wide" disabled={registry} hidden={registry}>
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
    verified_baseline_not_accepted: "Проверено Lean, без приемки",
    requires_review: "Требует рецензии",
    failed: "Ошибка",
    inconclusive: "Вывод не получен",
    interrupted: "Прервано",
  };
  const mathematical = {
    proof_verified: "Доказательство проверено",
    refutation_verified: "Отрицание проверено",
    inconclusive: "Вывод не получен",
  };
  const stageStatuses = {
    candidate: "Кандидат",
    failed: "Ошибка",
    cancelled: "Отменено",
    limit_reached: "Предел достигнут",
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
            <table className="comparison-table">
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
                  <tr key={`${r.case}-${r.mode}-${r.repetition || 0}`}>
                    <td>
                      {r.case}
                      {r.repetition ? ` · повтор ${r.repetition}` : ""}
                    </td>
                    <td>
                      {{
                        single: "Один исследователь",
                        team: "Фиксированная команда",
                        adaptive: "Адаптивная команда",
                        demand: "Специалисты по необходимости",
                        lean: "Lean без модели",
                      }[r.mode] || r.mode}
                    </td>
                    <td>
                      {statuses[r.status] || r.status}
                      {r.mathematical_status && (
                        <p>
                          {mathematical[r.mathematical_status] ||
                            r.mathematical_status}
                        </p>
                      )}
                      {r.verification && (
                        <p>
                          Lean:{" "}
                          {r.verification.status === "verified"
                            ? "проверено"
                            : "не подтверждено"}
                        </p>
                      )}
                      {!!r.stages?.length && (
                        <details>
                          <summary>Этапы</summary>
                          {r.error && (
                            <p className="usage-summary">{r.error}</p>
                          )}
                          {r.stages.map((stage, index) => (
                            <div key={index}>
                              <strong>
                                {roles[
                                  stage.role === "counter"
                                    ? "counterexample"
                                    : stage.role
                                ] ||
                                  (stage.role === "universal"
                                    ? "Исследователь"
                                    : stage.role)}
                              </strong>
                              <p>
                                {stageStatuses[stage.status] || stage.status}
                              </p>
                              <p>
                                Запросы: {stage.requests} · время:{" "}
                                {stage.seconds.toFixed(1)} с
                              </p>
                              <p>
                                Выходные токены:{" "}
                                {stage.usage && !stage.usage.incomplete
                                  ? stage.usage.output_tokens
                                  : "не полностью измерено"}
                              </p>
                              {!!stage.provider_http_status && (
                                <p>HTTP: {stage.provider_http_status}</p>
                              )}
                              {(stage.failure_code ||
                                stage.finish_reason ||
                                stage.limit_reason) && (
                                <p className="usage-summary">
                                  {[
                                    stage.failure_code,
                                    stage.finish_reason,
                                    stage.limit_reason,
                                  ]
                                    .filter(Boolean)
                                    .join(" · ")}
                                </p>
                              )}
                              <p className="usage-summary">
                                Профиль: {stage.profile_sha256}
                              </p>
                              <p className="usage-summary">
                                Результат: {stage.result_sha256}
                              </p>
                            </div>
                          ))}
                        </details>
                      )}
                    </td>
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

export function TeamFields({ live }) {
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
      <label>
        Исправления формата на команду
        <input
          name="format_repair_limit"
          type="number"
          min="0"
          max="2"
          defaultValue="0"
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
          <ResearchMethodSelect live={live} />
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
                          format_repair_limit: Number(f.format_repair_limit),
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
        aria-label="Извлечение типов Lean"
        onSubmit={(e) => {
          e.preventDefault();
          const f = Object.fromEntries(new FormData(e.currentTarget));
          act(() =>
            live.command("/lemmas/inspect", {
              library: f.library,
              domain: f.domain,
            }),
          );
        }}
      >
        <label>
          Собранная лемма
          <select name="library" required>
            <option value="">Выберите лемму</option>
            {(state.library || [])
              .filter((l) => l.status === "ready")
              .map((l) => (
                <option value={l.id} key={l.id}>
                  {state.entities.find((e) => e.id === l.lemma)?.title || l.id}
                </option>
              ))}
          </select>
        </label>
        <label>
          Область
          <input name="domain" required maxLength={200} />
        </label>
        <button
          className="button secondary"
          disabled={live.busy || !live.data.lean?.configured}
        >
          <Search size={15} />
          Извлечь типы Lean
        </button>
      </form>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Лемма</th>
              <th>Источник</th>
              <th>Заключение</th>
            </tr>
          </thead>
          <tbody>
            {(state.lemma_signatures || []).map((s) => (
              <tr key={s.id}>
                <td>
                  {state.library?.find((l) => l.id === s.library)?.module ||
                    s.library}
                </td>
                <td>{s.native?.status === "verified" ? "Lean" : "Оператор"}</td>
                <td>{s.conclusion}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
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
      <NativeLemmaMatching live={live} target={target} />
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
                  native_check: f.native_check === "on",
                  arguments:
                    f.native_check === "on"
                      ? (h.signature.native?.binders || [])
                          .map((b) => ({
                            name: b.name,
                            term: f[`arg_${b.name}`]?.trim(),
                          }))
                          .filter((b) => b.term)
                      : [],
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
              <input
                type="checkbox"
                name="native_check"
                disabled={!h.signature.native || !live.data.lean?.configured}
              />
              Проверить предложенную подстановку средствами Lean
            </label>
            {(h.signature.native?.binders || []).map((b) => (
              <label key={b.name}>
                {b.name}: {b.type}
                <input name={`arg_${b.name}`} maxLength={1000} />
              </label>
            ))}
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

function NativeLemmaMatching({ live, target }) {
  const state = live.data.state;
  const [error, setError] = useState("");
  const [matchTarget, setMatchTarget] = useState(target);
  const [catalog, setCatalog] = useState(null);
  const [catalogMode, setCatalogMode] = useState("single");
  useEffect(() => setMatchTarget(target), [target]);
  const libraries = (state.library || []).filter((l) => l.status === "ready");
  const open = state.entities.filter(
    (e) => e.formal_goal && !["accepted", "refuted"].includes(e.status),
  );
  const selectedTarget = open.some((e) => e.id === matchTarget)
    ? matchTarget
    : open[0]?.id || "";
  const matches = (state.lemma_matches || [])
    .filter((m) => m.target === selectedTarget)
    .slice(-20)
    .reverse();
  const statuses = {
    verified: "Терм проверен Lean",
    obligations: "Недостающие условия",
    no_match: "Типы не совпали",
    candidate_failed: "Терм не прошел проверку",
    failed: "Проверка не завершена",
  };
  return (
    <section className="process-section" aria-label="Подбор типов Lean">
      <h3>Подбор типов Lean</h3>
      <RepresentationTrial
        key={selectedTarget}
        live={live}
        target={selectedTarget}
        libraries={libraries}
      />
      <form
        className="runtime-form"
        aria-label="Поиск по каталогу Lean"
        onSubmit={async (e) => {
          e.preventDefault();
          setError("");
          const f = Object.fromEntries(new FormData(e.currentTarget));
          try {
            setCatalog(
              await live.command(
                catalogMode === "chain"
                  ? "/lemmas/chain-search"
                  : "/lemmas/catalog-search",
                {
                  target: selectedTarget,
                  query: f.query,
                  limit: Number(f.limit),
                  timeout_seconds: Number(f.timeout),
                  ...(catalogMode === "chain"
                    ? {
                        depth: Number(f.depth),
                        max_trials: Number(f.max_trials),
                      }
                    : {}),
                  confirm: true,
                },
              ),
            );
          } catch (err) {
            setError(err.message);
          }
        }}
      >
        <fieldset className="wide">
          <legend>Способ поиска</legend>
          <label className="checkbox-label">
            <input
              type="radio"
              name="catalog_mode"
              value="single"
              checked={catalogMode === "single"}
              onChange={() => setCatalogMode("single")}
            />
            Одна лемма
          </label>
          <label className="checkbox-label">
            <input
              type="radio"
              name="catalog_mode"
              value="chain"
              checked={catalogMode === "chain"}
              onChange={() => setCatalogMode("chain")}
            />
            Цепочка лемм
          </label>
        </fieldset>
        <label>
          Фильтр лемм
          <input name="query" maxLength={200} />
        </label>
        {catalogMode === "chain" && (
          <>
            <label>
              Глубина поиска Lean
              <input
                name="depth"
                type="number"
                min="1"
                max="3"
                defaultValue="3"
                required
              />
            </label>
            <label>
              Предел наборов
              <input
                name="max_trials"
                type="number"
                min="1"
                max="32"
                defaultValue="16"
                required
              />
            </label>
          </>
        )}
        <label>
          Предел лемм
          <input
            name="limit"
            type="number"
            min={1}
            max={8}
            defaultValue={4}
            required
          />
        </label>
        <label>
          Предел времени, с
          <input
            name="timeout"
            type="number"
            min={1}
            max={120}
            defaultValue={90}
            required
          />
        </label>
        <label className="checkbox-label">
          <input type="checkbox" required />
          Подтверждаю местные проверки каталога
        </label>
        <button
          className="button secondary"
          disabled={
            live.busy ||
            !libraries.length ||
            !selectedTarget ||
            !live.data.lean?.configured
          }
        >
          <Search size={15} />
          Проверить каталог
        </button>
      </form>
      {catalog && (
        <p role="status">
          Проверено лемм: {catalog.matches.length}.{" "}
          {catalog.status === "verified"
            ? "Терм проверен Lean"
            : catalog.status === "bounded"
              ? "Достигнут предел поиска"
              : "Проверки завершены"}
        </p>
      )}
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
      <form
        className="runtime-form"
        onSubmit={async (e) => {
          e.preventDefault();
          setError("");
          const f = Object.fromEntries(new FormData(e.currentTarget));
          try {
            const reuse = e.nativeEvent.submitter?.value === "reuse";
            if (
              reuse &&
              !window.confirm(
                "Выполнить две местные проверки Lean, каждую до 90 секунд, без обращений к модели?",
              )
            )
              return;
            await live.command(
              reuse ? "/lemmas/reuse-trial" : "/lemmas/match",
              {
                target: f.target,
                library: f.library,
                ...(reuse
                  ? { depth: 6, timeout_seconds: 90, confirm: true }
                  : {}),
              },
            );
          } catch (err) {
            setError(err.message);
          }
        }}
      >
        <label>
          Цель
          <select
            name="target"
            required
            value={selectedTarget}
            onChange={(e) => setMatchTarget(e.target.value)}
          >
            {open.map((e) => (
              <option key={e.id} value={e.id}>
                {e.title}
              </option>
            ))}
          </select>
        </label>
        <label>
          Лемма
          <select name="library" required>
            {libraries.map((l) => (
              <option key={l.id} value={l.id}>
                {state.entities.find((e) => e.id === l.lemma)?.title ||
                  l.module}
              </option>
            ))}
          </select>
        </label>
        <button
          className="button secondary"
          disabled={
            live.busy ||
            !libraries.length ||
            !open.length ||
            !live.data.lean?.configured
          }
        >
          <Search size={15} />
          Подобрать подстановку
        </button>
        <button
          className="button secondary"
          value="reuse"
          disabled={
            live.busy ||
            !libraries.length ||
            !open.length ||
            !live.data.lean?.configured
          }
        >
          <FlaskConical size={15} />
          Сравнить перенос леммы
        </button>
      </form>
      {matches.map((m) => {
        const current =
          state.entities.find((e) => e.id === m.target)?.revision ===
          m.target_revision;
        return (
          <div className="team-row" key={m.id}>
            <strong>{statuses[m.report.status] || m.report.status}</strong>
            <p>{state.library.find((l) => l.id === m.library)?.module}</p>
            {m.chain && (
              <p>
                Поиск цепочки: наборов {m.chain.trials.length} /{" "}
                {m.chain.max_trials}, глубина {m.chain.depth}.
              </p>
            )}
            {m.representation && (
              <div className="representation-result">
                <strong>Смена представления</strong>
                <p>
                  Исходная цель: {m.representation.original.status}. Прямая
                  цель: {m.representation.direct.status}. Эквивалентность:{" "}
                  {m.representation.bridge.status}. Перенос:{" "}
                  {m.representation.transferred?.status || "не выполнен"}.
                </p>
                <p>
                  Автор эквивалентности: {m.representation.bridge_author}.
                  Обращений к модели: {m.representation.model_calls}.
                </p>
                <details>
                  <summary>Протокол четырех проверок</summary>
                  <pre className="proof-source">
                    {JSON.stringify(m.representation, null, 2)}
                  </pre>
                </details>
              </div>
            )}
            {m.reuse && (
              <div className="reuse-summary">
                <p>
                  Без леммы:{" "}
                  {m.reuse.without.status === "verified"
                    ? "проверено"
                    : "не получено доказательство"}
                  , {m.reuse.without.seconds.toFixed(2)} с.
                </p>
                <p>
                  С леммой:{" "}
                  {m.reuse.with.status === "verified"
                    ? "проверено"
                    : "не получено доказательство"}
                  , {m.reuse.with.seconds.toFixed(2)} с.
                </p>
                <p>
                  Обращений к модели: {m.reuse.model_calls}. Приемка не
                  выполнена.
                </p>
                <details>
                  <summary>Два исходника и отчеты Lean</summary>
                  <pre className="proof-source">
                    {JSON.stringify(m.reuse, null, 2)}
                  </pre>
                </details>
              </div>
            )}
            {!!m.report.bindings.length && (
              <dl>
                {m.report.bindings.map((b, i) => (
                  <React.Fragment key={i}>
                    <dt>{b.name}</dt>
                    <dd>
                      <code>{b.term}</code>
                    </dd>
                  </React.Fragment>
                ))}
              </dl>
            )}
            {m.report.missing.map((b, i) => (
              <p key={i}>
                {b.name}: <code>{b.type}</code>
              </p>
            ))}
            {m.report.diagnostics && (
              <details>
                <summary>Диагностика Lean</summary>
                <pre>{m.report.diagnostics}</pre>
              </details>
            )}
            {m.application ? (
              <p>Создано обязательство: {m.application}</p>
            ) : !current ? (
              <p>Версия цели изменена</p>
            ) : (
              ["verified", "obligations"].includes(m.report.status) && (
                <form
                  className="runtime-form"
                  onSubmit={async (e) => {
                    e.preventDefault();
                    setError("");
                    try {
                      await live.command(`/lemmas/matches/${m.id}/apply`, {
                        confirm: true,
                      });
                    } catch (err) {
                      setError(err.message);
                    }
                  }}
                >
                  <label className="checkbox-label">
                    <input type="checkbox" required />
                    Подтверждаю создание обязательств
                  </label>
                  <button className="button secondary" disabled={live.busy}>
                    <Plus size={15} />
                    Создать обязательства
                  </button>
                </form>
              )
            )}
          </div>
        );
      })}
    </section>
  );
}
