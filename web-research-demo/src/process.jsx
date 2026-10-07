import React, { useEffect, useRef, useState } from "react";
import { api } from "./api.js";
import {
  Play,
  Pause,
  Square,
  Plus,
  X,
  ScrollText,
  ShieldCheck,
  Send,
  RotateCcw,
  Upload,
  Gauge,
} from "lucide-react";
import { BranchList, RefutationActions } from "./research-progress.jsx";
import { PlanningPanel } from "./research-management.jsx";
import { verificationCurrent } from "./proof-state.js";

const roles = {
  proof: "Поиск доказательства",
  counterexample: "Поиск контрпримеров",
  formalize: "Формализация",
  review: "Рецензия",
};
const states = {
  running: "В работе",
  paused: "Приостановлена",
  blocked: "Ожидает уточнения",
  awaiting_review: "Ожидает приемки",
  interrupted: "Прервана",
  completed: "Завершена",
  cancelled: "Остановлена",
  verified: "Проверено",
  failed: "Не прошло проверку",
  stale: "Устарело",
  queued: "В очереди",
};
const activeTeam = (status) =>
  ["running", "paused", "blocked", "awaiting_review"].includes(status);

export function TeamPanel({ live, study, all, onSelect }) {
  const [open, setOpen] = useState(false),
    [methods, setMethods] = useState([]),
    [mode, setMode] = useState("team"),
    [error, setError] = useState(""),
    [resume, setResume] = useState(null),
    [selectedStudy, setSelectedStudy] = useState(study || "");
  const { state, profiles, workspaces, lean } = live.data;
  useEffect(() => setSelectedStudy(study || ""), [study]);
  useEffect(() => {
    let active = true;
    api("/research-methods")
      .then((v) => {
        if (active) setMethods(v);
      })
      .catch((e) => {
        if (active) setError(e.message);
      });
    return () => {
      active = false;
    };
  }, []);
  const teams = (state.teams || []).filter((t) => all || t.study === study);
  async function act(fn) {
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(e.message);
    }
  }
  return (
    <section className="process-section" aria-label="Исследовательская команда">
      <div className="section-intro">
        <h2>Исследовательская команда</h2>
        <button
          className="button secondary"
          disabled={live.busy || state.paused}
          onClick={() => setOpen(!open)}
        >
          <Plus size={15} />
          Создать команду
        </button>
      </div>
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
      {!all && study && <StudyBudgetPanel live={live} study={study} />}
      {!all && study && <PlanningPanel live={live} study={study} />}
      {open && (
        <form
          className="runtime-form"
          aria-label="Настройка команды"
          onSubmit={(e) => {
            e.preventDefault();
            const v = Object.fromEntries(new FormData(e.currentTarget));
            act(async () => {
              const team = {
                study: v.study,
                target: v.target,
                profiles: Object.fromEntries(
                  Object.keys(roles).map((r) => [r, v[r]]),
                ),
                workspace: v.workspace,
                max_attempts: Number(v.max_attempts),
                require_lean: v.lean === "on",
                confirm: v.confirm === "on",
              };
              await live.command(
                mode === "branch" ? "/branches" : "/teams",
                mode === "branch"
                  ? {
                      team,
                      method: v.method,
                      rationale: v.rationale,
                      priority: Number(v.priority),
                      confirm: team.confirm,
                    }
                  : team,
              );
              setOpen(false);
            });
          }}
        >
          <label>
            Порядок запуска
            <select
              value={mode}
              onChange={(e) => setMode(e.target.value)}
              aria-label="Порядок запуска"
            >
              <option value="team">Отдельная команда</option>
              <option value="branch">Ветвь исследования</option>
            </select>
          </label>
          {mode === "branch" && (
            <>
              <label>
                Прием
                <select name="method" required>
                  {methods.map((m) => (
                    <option value={m.id} key={m.id}>
                      {m.label}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Приоритет
                <input
                  name="priority"
                  type="number"
                  min="0"
                  max="100"
                  defaultValue="50"
                  required
                />
              </label>
              <label className="wide">
                Основание выбора
                <textarea name="rationale" maxLength="4000" required />
              </label>
            </>
          )}
          <label>
            Исследование
            <select
              name="study"
              value={selectedStudy || state.studies[0]?.id || ""}
              onChange={(e) => setSelectedStudy(e.target.value)}
            >
              {state.studies.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.title}
                </option>
              ))}
            </select>
          </label>
          <label>
            Обязательство
            <select
              name="target"
              aria-label="Обязательство"
              key={selectedStudy}
              defaultValue=""
            >
              <option value="">Главная цель исследования</option>
              {state.entities
                .filter(
                  (e) =>
                    e.study === (selectedStudy || state.studies[0]?.id) &&
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
            Входные файлы
            <select name="workspace">
              {workspaces.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.label}
                </option>
              ))}
            </select>
          </label>
          {Object.entries(roles).map(([role, label]) => (
            <label key={role}>
              {label}
              <select
                name={role}
                aria-label={label}
                required
                defaultValue={
                  profiles.find((p) => p.id.includes(role) && p.available)
                    ?.id ||
                  profiles.find(
                    (p) =>
                      p.available &&
                      (role !== "review" || p.id !== profiles[0]?.id),
                  )?.id ||
                  ""
                }
              >
                <option value="" disabled>
                  Выберите профиль
                </option>
                {profiles.map((p) => (
                  <option value={p.id} key={p.id} disabled={!p.available}>
                    {p.label}
                  </option>
                ))}
              </select>
            </label>
          ))}
          <label>
            Предел попыток
            <input
              type="number"
              min="4"
              max="20"
              name="max_attempts"
              defaultValue="8"
              required
            />
          </label>
          <label className="checkbox">
            <input
              type="checkbox"
              name="lean"
              defaultChecked={!!lean?.configured}
              disabled={!lean?.configured}
            />
            Проверка Lean
          </label>
          <label className="checkbox wide">
            <input type="checkbox" name="confirm" required />
            Подтверждаю автоматические запуски в пределах бюджета и расходы
            модели
          </label>
          <button className="button primary" disabled={live.busy}>
            <Play size={15} />
            Начать
          </button>
        </form>
      )}
      <BranchList live={live} study={study} all={all} methods={methods} />
      {teams.map((t) => (
        <div className="team-row" key={t.id}>
          <div className="section-intro">
            <strong>
              {state.studies.find((s) => s.id === t.study)?.title}
            </strong>
            <span>
              {states[t.status] || t.status} · {t.used_attempts}/
              {t.max_attempts}
            </span>
          </div>
          <p>{t.reason}</p>
          {t.status === "blocked" &&
            ["exploring", "review", "refutation_review"].includes(t.stage) && (
              <button
                className="button secondary"
                disabled={live.busy}
                onClick={() => {
                  if (
                    window.confirm(
                      "Повторно обработать сохраненные ответы без повторного запроса той же роли?",
                    )
                  )
                    act(() =>
                      live.command(`/teams/${t.id}/commands`, {
                        kind: "reprocess",
                        confirm: true,
                        note: "Оператор разрешил повторную обработку сохраненного отчета.",
                      }),
                    );
                }}
              >
                <RotateCcw size={15} />
                Повторно обработать отчеты
              </button>
            )}
          <p>{state.entities.find((e) => e.id === t.goal)?.title}</p>
          <div className="role-strip">
            {Object.keys(roles)
              .filter((role) => t.profiles[role])
              .map((role) => {
                const p = t.profiles[role];
                const status = state.attempts.find(
                  (a) => a.id === t.current[role],
                )?.status;
                return (
                  <div key={role}>
                    <strong>{roles[role]}</strong>
                    <small>
                      {profiles.find((v) => v.id === p)?.label || p}
                    </small>
                    <span>
                      {{
                        candidate: "Кандидат",
                        limit_reached: "Достигнут предел",
                        preparing: "Подготовка",
                        timed_out: "Истекло время",
                        awaiting_worker: "Ожидает исполнителя",
                        cancelled: "Остановлено",
                      }[status] ||
                        states[status] ||
                        status ||
                        "Не назначено"}
                    </span>
                  </div>
                );
              })}
          </div>
          <div className="row-actions">
            {t.target && (
              <button className="text-link" onClick={() => onSelect(t.target)}>
                Утверждение
              </button>
            )}
            {["running", "awaiting_review"].includes(t.status) && (
              <button
                className="icon-button"
                title="Приостановить новые задания"
                onClick={() =>
                  act(() =>
                    live.command("/teams/" + t.id + "/commands", {
                      kind: "pause",
                    }),
                  )
                }
              >
                <Pause size={16} />
              </button>
            )}
            {["paused", "blocked", "interrupted"].includes(t.status) && (
              <button className="button secondary" onClick={() => setResume(t)}>
                <RotateCcw size={15} />
                Продолжить команду
              </button>
            )}
            {activeTeam(t.status) && (
              <button
                className="icon-button"
                title="Остановить команду"
                onClick={() =>
                  act(() =>
                    live.command("/teams/" + t.id + "/commands", {
                      kind: "stop",
                    }),
                  )
                }
              >
                <Square size={16} />
              </button>
            )}
          </div>
        </div>
      ))}
      {resume && (
        <form
          className="runtime-form"
          aria-label="Продолжение команды"
          onSubmit={(e) => {
            e.preventDefault();
            const v = Object.fromEntries(new FormData(e.currentTarget));
            const kind = e.nativeEvent.submitter?.value || "resume";
            const limits = {};
            Object.entries(resume.profiles).forEach(([role, p]) => {
              const baseline =
                resume.role_limits?.[role] ||
                profiles.find((x) => x.id === p)?.limits;
              if (baseline?.max_steps > 0)
                limits[role] = {
                  ...baseline,
                  max_steps: Number(v[role + "_steps"]),
                  timeout_seconds: Number(v[role + "_time"]),
                  max_output_tokens: Number(v[role + "_tokens"]),
                };
            });
            act(async () => {
              await live.command("/teams/" + resume.id + "/commands", {
                kind,
                confirm: v.confirm === "on",
                max_attempts: Number(v.max_attempts),
                role_limits: limits,
                note: v.note,
              });
              setResume(null);
            });
          }}
        >
          <label>
            Общий предел попыток
            <input
              name="max_attempts"
              type="number"
              min={Math.max(4, resume.used_attempts)}
              max="20"
              required
              defaultValue={resume.max_attempts}
            />
          </label>
          {Object.entries(resume.profiles).map(([role, p]) => {
            const limits =
              resume.role_limits?.[role] ||
              profiles.find((x) => x.id === p)?.limits;
            return (
              limits?.max_steps > 0 && (
                <React.Fragment key={role}>
                  <label>
                    {roles[role]}: шаги
                    <input
                      name={role + "_steps"}
                      type="number"
                      min="1"
                      max="100"
                      defaultValue={limits.max_steps}
                    />
                  </label>
                  <label>
                    Время, с
                    <input
                      name={role + "_time"}
                      type="number"
                      min="1"
                      max="3600"
                      defaultValue={limits.timeout_seconds}
                    />
                  </label>
                  <label>
                    {roles[role]}: токенов на ответ
                    <input
                      name={role + "_tokens"}
                      type="number"
                      min="1"
                      max="1000000"
                      required
                      defaultValue={limits.max_output_tokens}
                    />
                  </label>
                </React.Fragment>
              )
            );
          })}
          <label className="wide">
            Уточнение команде
            <textarea
              name="note"
              maxLength={4000}
              defaultValue={resume.operator_note || ""}
            />
          </label>
          <label className="checkbox wide">
            <input type="checkbox" name="confirm" required />
            Подтверждаю ограничения и возможные расходы при запуске
          </label>
          <button
            className="button primary"
            value="resume"
            disabled={live.busy}
          >
            <Play size={15} />
            Продолжить
          </button>
          <button
            className="button secondary"
            value="configure"
            disabled={live.busy}
          >
            <ShieldCheck size={15} />
            Сохранить ограничения
          </button>
          <button
            className="button secondary"
            type="button"
            onClick={() => setResume(null)}
          >
            <X size={15} />
            Закрыть
          </button>
        </form>
      )}
    </section>
  );
}

function StudyBudgetPanel({ live, study }) {
  const [open, setOpen] = useState(false);
  const [observation, setObservation] = useState(null);
  const [error, setError] = useState("");
  const selected = live.data.state.studies.find((s) => s.id === study);
  useEffect(() => {
    let alive = true;
    api("/studies/" + study + "/observability")
      .then((v) => {
        if (alive) setObservation({ study, budget: v.budget });
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    return () => {
      alive = false;
    };
  }, [study, live.data.state.revision]);
  const budget = observation?.study === study ? observation.budget : null;
  return (
    <div className="study-budget" aria-label="Общий бюджет исследования">
      <div className="section-intro">
        <strong>Бюджет исследования</strong>
        <button
          className="button secondary"
          disabled={live.busy}
          onClick={() => {
            setError("");
            setOpen(!open);
          }}
        >
          <Gauge size={15} /> Настроить бюджет
        </button>
      </div>
      {budget && (
        <p>
          Попытки: {budget.attempts}/
          {selected?.budget?.max_attempts || "без общего предела"}. Известные
          выходные токены: {budget.known_output_tokens}. Учтено с удержанным
          резервом: {budget.charged_tokens}, резерв действующих попыток:{" "}
          {budget.reserved_tokens}. Неизвестный расход: {budget.unknown_usage}.
        </p>
      )}
      {budget && (
        <p>
          Запросы модели: {budget.known_model_requests} известных; учтено{" "}
          {budget.charged_model_requests}, зарезервировано{" "}
          {budget.reserved_model_requests}. Общий предел:{" "}
          {selected?.budget?.max_model_requests || "не задан"}.
        </p>
      )}
      {selected?.budget && (
        <p>
          Предел выходных токенов:{" "}
          {selected.budget.max_output_tokens || "не задан"}.
        </p>
      )}
      {selected?.budget?.deadline_at && (
        <p>
          Срок: {new Date(selected.budget.deadline_at).toLocaleString("ru-RU")}
        </p>
      )}
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
      {open && (
        <form
          className="runtime-form"
          aria-label="Настройка общего бюджета"
          key={study}
          onSubmit={async (e) => {
            e.preventDefault();
            const v = Object.fromEntries(new FormData(e.currentTarget));
            setError("");
            try {
              await live.command("/studies/" + study + "/budget", {
                budget: {
                  max_attempts: Number(v.attempts),
                  max_model_requests: Number(v.requests),
                  max_output_tokens: Number(v.tokens),
                  deadline_at: v.deadline
                    ? new Date(v.deadline).toISOString()
                    : null,
                },
                confirm: v.confirm === "on",
                note: v.note,
              });
              setOpen(false);
            } catch (error) {
              setError(error.message);
            }
          }}
        >
          <label>
            Попыток на исследование
            <input
              name="attempts"
              aria-label="Попыток на исследование"
              type="number"
              min="1"
              max="100"
              defaultValue={
                selected?.budget?.max_attempts ||
                Math.max(budget?.attempts || 0, 20)
              }
              required
            />
          </label>
          <label>
            Запросов модели на исследование
            <input
              name="requests"
              aria-label="Запросов модели на исследование"
              type="number"
              min="0"
              max="10000"
              defaultValue={selected?.budget?.max_model_requests || 0}
              required
            />
          </label>
          <label>
            Выходных токенов на исследование
            <input
              name="tokens"
              aria-label="Выходных токенов на исследование"
              type="number"
              min="0"
              max="1000000000"
              defaultValue={selected?.budget?.max_output_tokens || 0}
              required
            />
          </label>
          <label>
            Срок завершения
            <input
              name="deadline"
              type="datetime-local"
              defaultValue={
                selected?.budget?.deadline_at
                  ? new Date(
                      new Date(selected.budget.deadline_at).getTime() -
                        new Date(
                          selected.budget.deadline_at,
                        ).getTimezoneOffset() *
                          60000,
                    )
                      .toISOString()
                      .slice(0, 16)
                  : ""
              }
            />
          </label>
          <label className="wide">
            Основание изменения
            <textarea name="note" required maxLength="4000" />
          </label>
          <label className="checkbox wide">
            <input name="confirm" type="checkbox" required />
            Подтверждаю общий бюджет и возможные расходы
          </label>
          <button className="button primary" disabled={live.busy}>
            <Gauge size={15} />
            Сохранить бюджет
          </button>
        </form>
      )}
    </div>
  );
}

export function FormalGoalPanel({ live, selected, study }) {
  const { state } = live.data;
  const entity =
    state.entities.find((e) => e.id === selected && e.study === study) ||
    state.entities.find(
      (e) => e.id === state.studies.find((s) => s.id === study)?.goal,
    );
  const [open, setOpen] = useState(false),
    [sourceOpen, setSourceOpen] = useState(false),
    [error, setError] = useState("");
  if (!entity) return null;
  return (
    <section className="process-section">
      <div className="section-intro">
        <h2>Формальная постановка</h2>
        <button
          className="button secondary"
          disabled={
            live.busy || ["accepted", "refuted"].includes(entity.status)
          }
          onClick={() => {
            setOpen(!open);
            setSourceOpen(false);
          }}
        >
          <ShieldCheck size={15} />
          {entity.formal_goal ? "Изменить" : "Закрепить цель Lean"}
        </button>
      </div>
      <p>
        {entity.title} · версия {entity.revision}
      </p>
      {entity.formal_goal && (
        <code>
          {entity.formal_goal.declaration} → {entity.formal_goal.candidate}
        </code>
      )}
      {entity.formal_goal && (
        <button
          className="button secondary"
          disabled={
            live.busy ||
            !live.data.lean?.configured ||
            ["accepted", "refuted"].includes(entity.status)
          }
          onClick={() => {
            setSourceOpen(!sourceOpen);
            setOpen(false);
          }}
        >
          <Upload size={15} />
          Зарегистрировать файл Lean
        </button>
      )}
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {open && (
        <form
          key={entity.id}
          className="runtime-form"
          onSubmit={async (e) => {
            e.preventDefault();
            const v = Object.fromEntries(new FormData(e.currentTarget));
            setError("");
            try {
              await live.command("/entities/" + entity.id + "/formal-goal", {
                libraries: new FormData(e.currentTarget).getAll("libraries"),
                goal: {
                  source: v.source,
                  declaration: v.declaration,
                  candidate: v.candidate,
                },
              });
              setOpen(false);
            } catch (err) {
              setError(err.message);
            }
          }}
        >
          <label>
            Имя цели
            <input
              name="declaration"
              required
              defaultValue={entity.formal_goal?.declaration || "Statement"}
            />
          </label>
          <label>
            Имя теоремы кандидата
            <input
              name="candidate"
              required
              defaultValue={entity.formal_goal?.candidate || "Candidate"}
            />
          </label>
          <label className="wide">
            Исходный текст Goal.lean
            <textarea
              className="source-editor"
              name="source"
              required
              maxLength={65536}
              defaultValue={entity.formal_goal?.source || ""}
            />
          </label>
          {(live.data.state.library || [])
            .filter(
              (l) =>
                l.status === "ready" &&
                live.data.state.entities.find((e) => e.id === l.lemma)
                  ?.status === "accepted",
            )
            .map((l) => (
              <label className="checkbox wide" key={l.id}>
                <input
                  name="libraries"
                  type="checkbox"
                  value={l.id}
                  defaultChecked={entity.dependencies.includes(l.lemma)}
                />
                {live.data.state.entities.find((e) => e.id === l.lemma)?.title}{" "}
                · версия {l.lemma_revision}
              </label>
            ))}
          <button className="button primary" disabled={live.busy}>
            <ShieldCheck size={15} />
            Закрепить версию
          </button>
        </form>
      )}
      {sourceOpen && (
        <form
          key={entity.id + "-source"}
          className="runtime-form"
          aria-label="Регистрация доказательства"
          onSubmit={async (e) => {
            e.preventDefault();
            const values = Object.fromEntries(new FormData(e.currentTarget));
            setError("");
            try {
              await live.command(
                "/entities/" + entity.id + "/proof-source",
                values,
              );
              setSourceOpen(false);
            } catch (err) {
              setError(err.message);
            }
          }}
        >
          <label>
            Автор
            <input name="author" required maxLength={300} />
          </label>
          <label className="wide">
            Происхождение
            <textarea name="basis" required maxLength={4000} />
          </label>
          <label className="wide">
            Исходный текст Candidate.lean
            <textarea
              className="source-editor"
              name="source"
              required
              maxLength={64000}
            />
          </label>
          <button className="button primary" disabled={live.busy}>
            <ShieldCheck size={15} />
            Зарегистрировать и проверить
          </button>
        </form>
      )}
      {(state.verifications || [])
        .filter((v) => v.target === entity.id)
        .slice()
        .reverse()
        .map((v) => (
          <details className="verification-row" key={v.id}>
            <summary>
              {v.purpose === "refutation"
                ? "Отрицание · "
                : "Доказательство · "}
              {!verificationCurrent(v, entity, state)
                ? "Устарело"
                : states[v.status] || v.status}{" "}
              · {v.report?.phase || "Ожидание"} · версия {v.target_revision}
            </summary>
            {v.origin === "submitted" && (
              <p>
                Автор: {v.author} · исходная версия {v.submitted_revision}
                <br />
                Происхождение: {v.basis}
              </p>
            )}
            <pre>
              {v.report?.diagnostics ||
                (v.report
                  ? "Проверка завершена."
                  : "Диагностика пока отсутствует")}
            </pre>
            <small>
              Цель: {v.report?.goal_sha256}
              <br />
              Исходный текст: {v.report?.source_sha256}
              <br />
              Среда: {v.report?.environment_sha256}
              <br />
              Аксиомы: {(v.report?.axioms || []).join(", ") || "Нет данных"}
            </small>
            {v.origin === "submitted" && (
              <details>
                <summary>Исходный текст</summary>
                <pre>{v.source}</pre>
              </details>
            )}
            {v.origin === "submitted" &&
              v.purpose !== "refutation" &&
              v.status === "verified" &&
              verificationCurrent(v, entity, state) &&
              entity.proofVerification !== v.id &&
              !["accepted", "refuted"].includes(entity.status) && (
                <button
                  className="button secondary"
                  disabled={live.busy}
                  onClick={async () => {
                    setError("");
                    try {
                      await live.command(
                        "/verifications/" + v.id + "/attach",
                        {},
                      );
                    } catch (err) {
                      setError(err.message);
                    }
                  }}
                >
                  <Send size={15} />
                  Направить на приемку
                </button>
              )}
            {v.purpose === "refutation" && (
              <RefutationActions live={live} entity={entity} verification={v} />
            )}
          </details>
        ))}
    </section>
  );
}

export function JournalPanel({ live, attempt, onClose }) {
  const [rows, setRows] = useState([]),
    [error, setError] = useState("");
  const sequence = useRef(0);
  useEffect(() => {
    let disposed = false,
      inFlight = false;
    sequence.current = 0;
    setRows([]);
    setError("");
    const poll = async () => {
      if (inFlight) return;
      inFlight = true;
      try {
        const page = await live.get(
          "/attempts/" + attempt.id + "/events?after=" + sequence.current,
        );
        if (!disposed && page.length) {
          sequence.current = page.at(-1).sequence;
          setRows((old) => [...old, ...page].slice(-300));
        }
      } catch (e) {
        if (!disposed) setError(e.message);
      } finally {
        inFlight = false;
      }
    };
    poll();
    const timer = setInterval(poll, 1000);
    return () => {
      disposed = true;
      clearInterval(timer);
    };
  }, [attempt.id]);
  return (
    <section
      className="process-section journal-panel"
      aria-label="Журнал попытки"
    >
      <div className="section-intro">
        <h2>Журнал попытки</h2>
        <button
          className="icon-button"
          title="Закрыть журнал"
          onClick={onClose}
        >
          <X size={16} />
        </button>
      </div>
      <small>{attempt.id}</small>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {!rows.length && <p className="muted">Записанных действий пока нет</p>}
      {rows.map((e) => (
        <div className="journal-event" key={e.sequence}>
          <div>
            <time>{new Date(e.at).toLocaleTimeString("ru-RU")}</time>
            <strong>
              {{
                attempt_started: "Начало",
                model_requested: "Запрос модели",
                model_response: "Ответ модели",
                assistant_message: "Материал агента",
                tool_started: "Вызов инструмента",
                tool_completed: "Результат инструмента",
                candidate: "Кандидат",
                limit_reached: "Предел достигнут",
                executor_diagnostic: "Диагностика исполнителя",
              }[e.type] || e.type}
            </strong>
            <span>
              {e.tool} {e.status}
            </span>
          </div>
          {e.usage && (
            <small>
              Токены: <UsageSummary usage={e.usage} />
            </small>
          )}
          {e.input && (
            <details>
              <summary>Входные данные</summary>
              <pre>{e.input}</pre>
            </details>
          )}
          {e.output && <pre>{e.output}</pre>}
        </div>
      ))}
    </section>
  );
}
export function UsageSummary({ usage }) {
  if (!usage) return <>Не сообщен</>;
  return (
    <span className="usage-summary">
      Вход: {usage.input_tokens.toLocaleString("ru-RU")} · Выход:{" "}
      {usage.output_tokens.toLocaleString("ru-RU")} · Итого:{" "}
      {(usage.input_tokens + usage.output_tokens).toLocaleString("ru-RU")}
      {usage.incomplete && <small>Неполные сведения о расходе</small>}
      {usage.source && (
        <small>
          Источник:{" "}
          {{
            coddy_token_usage: "уведомления Coddy",
            acp_response: "ответ ACP",
            model_api: "API модели",
          }[usage.source] || usage.source}
        </small>
      )}
    </span>
  );
}

export function ResumeForm({ live, attempt, onClose }) {
  const [error, setError] = useState("");
  const limits =
    attempt.limits ||
    live.data.profiles.find((p) => p.id === attempt.profile)?.limits;
  return (
    <form
      className="runtime-form"
      aria-label="Продолжение попытки"
      onSubmit={async (e) => {
        e.preventDefault();
        const v = Object.fromEntries(new FormData(e.currentTarget));
        setError("");
        const next =
          limits?.max_steps > 0
            ? {
                ...limits,
                max_steps: Number(v.steps),
                timeout_seconds: Number(v.timeout),
                max_output_tokens: Number(v.tokens),
              }
            : limits;
        try {
          await live.command("/attempts/" + attempt.id + "/resume", {
            limits: next,
            note: v.note,
            confirm: v.confirm === "on",
          });
          onClose();
        } catch (err) {
          setError(err.message);
        }
      }}
    >
      {error && (
        <p className="api-error wide" role="alert">
          {error}
        </p>
      )}
      {limits?.max_steps > 0 && (
        <>
          <label>
            Предел шагов
            <input
              name="steps"
              type="number"
              min="1"
              max="100"
              defaultValue={limits.max_steps}
            />
          </label>
          <label>
            Токенов на ответ
            <input
              name="tokens"
              type="number"
              min="1"
              max="1000000"
              defaultValue={limits.max_output_tokens}
            />
          </label>
          <label>
            Время, с
            <input
              name="timeout"
              type="number"
              min="1"
              max="3600"
              defaultValue={limits.timeout_seconds}
            />
          </label>
        </>
      )}
      <label className="wide">
        Уточнение
        <textarea name="note" maxLength={4000} />
      </label>
      <label className="checkbox wide">
        <input type="checkbox" name="confirm" required />
        Подтверждаю новую попытку и расходы модели
      </label>
      <button className="button primary" disabled={live.busy}>
        <Play size={15} />
        Продолжить
      </button>
      <button type="button" className="button secondary" onClick={onClose}>
        <X size={15} />
        Закрыть
      </button>
    </form>
  );
}

export function TelegramPanel({ live, study }) {
  const [error, setError] = useState(""),
    [preview, setPreview] = useState(null);
  const channels = live.data.telegram || [],
    state = live.data.state;
  async function act(fn) {
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(e.message);
    }
  }
  return (
    <section className="process-section">
      <div className="section-intro">
        <h2>Telegram</h2>
        <span>
          {channels.length ? "Каналы исследований" : "Подключение не настроено"}
        </span>
        <button
          className="button secondary"
          disabled={!study || live.busy}
          onClick={() =>
            act(async () =>
              setPreview(await api("/studies/" + study + "/report")),
            )
          }
        >
          <ScrollText size={15} />
          Просмотреть отчет
        </button>
      </div>
      {preview?.study === study && (
        <pre
          className="study-report"
          role="region"
          aria-label="Отчет исследования"
        >
          {preview.text}
        </pre>
      )}
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
      {channels.map((c) => {
        const enabled = state.bindings?.some(
          (b) => b.study === study && b.connector === c.id && b.enabled,
        );
        return (
          <div className="connector-row" key={c.id}>
            <label className="checkbox">
              <input
                type="checkbox"
                checked={!!enabled}
                disabled={live.busy || !c.available}
                onChange={(e) =>
                  act(() =>
                    live.command("/telegram/bindings", {
                      study,
                      connector: c.id,
                      enabled: e.target.checked,
                    }),
                  )
                }
              />
              {c.label || c.id}
            </label>
            {enabled && (
              <form
                className="runtime-form"
                onSubmit={(e) => {
                  e.preventDefault();
                  const v = Object.fromEntries(new FormData(e.currentTarget));
                  act(() =>
                    live.command("/telegram/messages", {
                      study,
                      connector: c.id,
                      text: v.text,
                      confirm: v.confirm === "on",
                    }),
                  );
                }}
              >
                <label className="wide">
                  Сообщение
                  <textarea name="text" required maxLength={4000} />
                </label>
                <label className="checkbox wide">
                  <input type="checkbox" name="confirm" required />
                  Разрешаю отправить текст в выбранный чат
                </label>
                <button className="button secondary" disabled={live.busy}>
                  <Send size={15} />
                  Отправить
                </button>
                <button
                  type="button"
                  className="button secondary"
                  onClick={() =>
                    act(() =>
                      live.command("/telegram/messages", {
                        study,
                        connector: c.id,
                        status_report: true,
                        confirm: true,
                      }),
                    )
                  }
                >
                  <ScrollText size={15} />
                  Отправить состояние
                </button>
              </form>
            )}
          </div>
        );
      })}
      {(state.messages || [])
        .filter((m) => m.study === study)
        .slice(-30)
        .reverse()
        .map((m) => (
          <div className="journal-event" key={m.id}>
            <small>
              {m.direction === "incoming" ? "Входящее" : "Исходящее"} ·{" "}
              {m.state}
            </small>
            <p>{m.text}</p>
          </div>
        ))}
    </section>
  );
}

export function WorkerPanel({ live, study }) {
  const [error, setError] = useState("");
  const workers = live.data.workers || [];
  return (
    <section className="process-section">
      <div className="section-intro">
        <h2>Удаленные исполнители</h2>
        <span>
          {workers.length
            ? "Подключения исследования"
            : "Подключение не настроено"}
        </span>
      </div>
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
      {workers.map((w) => (
        <label key={w.id} className="checkbox">
          <input
            type="checkbox"
            disabled={!w.available || live.busy}
            checked={
              !!live.data.state.bindings?.some(
                (b) =>
                  b.kind === "worker" &&
                  b.study === study &&
                  b.connector === w.id &&
                  b.enabled,
              )
            }
            onChange={async (e) => {
              setError("");
              try {
                await live.command("/worker-bindings", {
                  study,
                  connector: w.id,
                  enabled: e.target.checked,
                });
              } catch (err) {
                setError(err.message);
              }
            }}
          />
          {w.label || w.id}
        </label>
      ))}
    </section>
  );
}
