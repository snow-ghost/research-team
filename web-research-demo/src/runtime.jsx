import React, { useEffect, useState } from "react";
import { CyclePanel } from "./cycles.jsx";
import { EvidencePanel } from "./evidence.jsx";
import { MemoryPanel } from "./research-progress.jsx";
import {
  TeamPanel,
  FormalGoalPanel,
  JournalPanel,
  ResumeForm,
  TelegramPanel,
  WorkerPanel,
  UsageSummary,
} from "./process.jsx";
import {
  Play,
  Square,
  Plus,
  FileText,
  Check,
  CircleAlert,
  Cpu,
  RefreshCw,
  ScrollText,
  RotateCcw,
  ShieldCheck,
  Download,
} from "lucide-react";

export const attemptStates = {
  queued: "В очереди",
  preparing: "Подготовка снимка",
  running: "В работе",
  cancelling: "Остановка",
  candidate: "Кандидат",
  failed: "Ошибка",
  cancelled: "Остановлено",
  timed_out: "Истекло время",
  limit_reached: "Достигнут предел",
  interrupted: "Прервано; исход требует проверки",
  awaiting_worker: "Ожидает удаленного исполнителя",
};
const running = (status) =>
  ["queued", "preparing", "running", "cancelling", "awaiting_worker"].includes(
    status,
  );

export function RuntimePanel({
  live,
  study,
  selected,
  providers = false,
  onSelect,
}) {
  const { state, profiles, workspaces } = live.data;
  const [taskForm, setTaskForm] = useState(false);
  const [launch, setLaunch] = useState("");
  const [result, setResult] = useState(null);
  const [error, setError] = useState("");
  const [journal, setJournal] = useState(null);
  const [resume, setResume] = useState(null);
  const remoteAvailable = (profile) =>
    (live.data.workers || []).some(
      (w) =>
        w.available &&
        w.profiles.includes(profile) &&
        state.bindings?.some(
          (b) =>
            b.kind === "worker" &&
            b.study === study &&
            b.connector === w.id &&
            b.enabled,
        ),
    );
  const tasks = state.tasks.filter(
    (t) =>
      providers ||
      state.entities.find((e) => e.id === t.target)?.study === study,
  );
  const attempts = state.attempts.filter(
    (a) =>
      providers ||
      state.entities.find((e) => e.id === a.target)?.study === study,
  );
  async function act(callback) {
    setError("");
    try {
      await callback();
    } catch (e) {
      setError(e.message);
    }
  }
  useEffect(() => {
    if (launch && state.tasks.find((t) => t.id === launch)?.attempt)
      setLaunch("");
  }, [state, launch]);
  return (
    <div className="scroll-content runtime-panel">
      <TeamPanel
        live={live}
        study={study}
        all={providers}
        onSelect={onSelect}
      />
      {!providers && (
        <FormalGoalPanel live={live} study={study} selected={selected} />
      )}
      {!providers && (
        <EvidencePanel live={live} study={study} selected={selected} />
      )}
      {!providers && (
        <MemoryPanel
          live={live}
          study={study}
          selected={selected}
          onSelect={onSelect}
        />
      )}
      <CyclePanel
        live={live}
        study={study}
        all={providers}
        onSelect={onSelect}
      />
      {journal && (
        <JournalPanel
          live={live}
          attempt={journal}
          onClose={() => setJournal(null)}
        />
      )}
      {resume && (
        <ResumeForm
          live={live}
          attempt={resume}
          onClose={() => setResume(null)}
        />
      )}
      {providers && (
        <>
          <div className="section-intro">
            <h2>Исполнители</h2>
            <span>{profiles.length} профилей</span>
          </div>
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Профиль</th>
                  <th>Программа</th>
                  <th>Модель</th>
                  <th>Навыки</th>
                  <th>Доступность</th>
                </tr>
              </thead>
              <tbody>
                {profiles.map((p) => (
                  <tr key={p.id}>
                    <td>
                      <Cpu size={14} /> {p.label}
                      <small>{p.id}</small>
                    </td>
                    <td>{p.provider}</td>
                    <td>{p.model}</td>
                    <td>{p.skills.join(", ") || "Нет"}</td>
                    <td>{p.available ? "Доступен" : "Требуется настройка"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      {error && (
        <p className="api-error" role="alert">
          <CircleAlert size={16} />
          {error}
        </p>
      )}
      <div className="section-intro">
        <h2>Задания</h2>
        <button
          className="button secondary"
          disabled={live.busy || !state.entities.length}
          onClick={() => setTaskForm(!taskForm)}
        >
          <Plus size={15} />
          Новое задание
        </button>
      </div>
      {taskForm && (
        <form
          className="runtime-form"
          onSubmit={(e) => {
            e.preventDefault();
            const values = Object.fromEntries(new FormData(e.currentTarget));
            act(async () => {
              await live.action({ type: "TASK", ...values });
              setTaskForm(false);
            });
          }}
        >
          <label>
            Утверждение
            <select
              name="target"
              defaultValue={selected || state.studies[0]?.goal}
            >
              {state.entities.map((e) => (
                <option value={e.id} key={e.id}>
                  {e.title}
                </option>
              ))}
            </select>
          </label>
          <label>
            Вид задания
            <select name="kind" defaultValue="proof">
              <option value="decompose">Предложение разбиения</option>
              <option value="proof">Поиск доказательства</option>
              <option value="review">Рецензия</option>
              <option value="counterexample">Поиск контрпримера</option>
              <option value="formalize">Формализация</option>
            </select>
          </label>
          <label>
            Название
            <input name="title" required maxLength={300} />
          </label>
          <label className="wide">
            Цель
            <textarea name="text" required maxLength={16000} />
          </label>
          <button className="button primary" disabled={live.busy}>
            <Plus size={15} />
            Создать задание
          </button>
        </form>
      )}
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Задание</th>
              <th>Вид</th>
              <th>Состояние</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {tasks.map((t) => (
              <tr key={t.id}>
                <td>
                  <button
                    className="text-link"
                    onClick={() => onSelect(t.target)}
                  >
                    {t.title}
                  </button>
                  <small>{t.id}</small>
                </td>
                <td>
                  {
                    {
                      proof: "Доказательство",
                      review: "Рецензия",
                      counterexample: "Контрпример",
                      answer: "Ответ",
                      formalize: "Формализация",
                    }[t.kind]
                  }
                </td>
                <td>{attemptStates[t.state] || t.state}</td>
                <td>
                  <button
                    className="icon-button"
                    title="Настроить запуск"
                    aria-label={"Настроить запуск " + t.title}
                    disabled={live.busy || !!t.attempt || state.paused}
                    onClick={() => setLaunch(t.id)}
                  >
                    <Play size={16} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {launch && (
        <form
          className="runtime-form"
          aria-label="Запуск задания"
          onSubmit={(e) => {
            e.preventDefault();
            const values = Object.fromEntries(new FormData(e.currentTarget));
            act(() =>
              live.start({
                task_id: launch,
                profile: values.profile,
                workspace: values.workspace,
                remote_worker: values.remote_worker || "",
                review_verification: values.review_verification || "",
                confirm: values.confirm === "on",
              }),
            );
          }}
        >
          {state.tasks.find((t) => t.id === launch)?.kind === "review" && (
            <label>
              Проверенный исходник
              <select
                name="review_verification"
                aria-label="Проверенный исходник"
                defaultValue=""
              >
                <option value="">Без привязки к формальному файлу</option>
                {(state.verifications || [])
                  .filter(
                    (v) =>
                      v.status === "verified" &&
                      v.target ===
                        state.tasks.find((t) => t.id === launch)?.target &&
                      v.target_revision ===
                        state.entities.find((e) => e.id === v.target)
                          ?.revision &&
                      (v.origin !== "submitted" ||
                        v.submitted_revision === v.target_revision),
                  )
                  .map((v) => (
                    <option key={v.id} value={v.id}>
                      {v.id} · {v.author || "Исполнитель"}
                    </option>
                  ))}
              </select>
            </label>
          )}
          <label>
            Исполнитель
            <select
              name="profile"
              aria-label="Исполнитель"
              required
              defaultValue={profiles.find((p) => p.available)?.id || ""}
            >
              <option value="" disabled>
                Выберите профиль
              </option>
              {profiles.map((p) => (
                <option
                  key={p.id}
                  value={p.id}
                  disabled={!p.available && !remoteAvailable(p.id)}
                >
                  {p.label}
                  {p.available
                    ? ""
                    : remoteAvailable(p.id)
                      ? " (удаленно)"
                      : " (недоступен)"}
                </option>
              ))}
            </select>
          </label>
          <label>
            Входные файлы
            <select name="workspace" required>
              {workspaces.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.label}
                </option>
              ))}
            </select>
          </label>
          <label className="checkbox wide">
            <input type="checkbox" name="confirm" required />
            Подтверждаю запуск и возможные расходы модели
          </label>
          <label>
            Место исполнения
            <select name="remote_worker">
              <option value="">Этот сервер</option>
              {(live.data.workers || [])
                .filter(
                  (w) =>
                    w.available &&
                    state.bindings?.some(
                      (b) =>
                        b.kind === "worker" &&
                        b.study === study &&
                        b.connector === w.id &&
                        b.enabled,
                    ),
                )
                .map((w) => (
                  <option key={w.id} value={w.id}>
                    {w.label || w.id}
                  </option>
                ))}
            </select>
          </label>
          <button
            className="button primary"
            disabled={
              live.busy ||
              !profiles.some((p) => p.available || remoteAvailable(p.id)) ||
              !workspaces.length
            }
          >
            <Play size={15} />
            Запустить
          </button>
          <button
            className="button secondary"
            type="button"
            onClick={() => setLaunch("")}
          >
            Отмена
          </button>
        </form>
      )}
      <div className="section-intro">
        <h2>Попытки</h2>
        <span>{attempts.length}</span>
      </div>
      {!attempts.length && <p className="muted">Запусков пока нет</p>}
      {attempts
        .slice()
        .reverse()
        .map((a) => (
          <article className="attempt-row" key={a.id}>
            <div>
              <strong>
                {state.tasks.find((t) => t.id === a.task_id)?.title}
              </strong>
              <small>{a.id}</small>
              <span>
                {a.profile} · {attemptStates[a.status] || a.status}
              </span>
              <small>
                Входной снимок v{a.input_snapshot} · Удаленный исход:{" "}
                {a.remote_outcome}
              </small>
            </div>
            <div className="row-actions">
              <button
                className="icon-button"
                title="Журнал действий"
                aria-label={"Журнал действий " + a.id}
                onClick={() => setJournal(a)}
              >
                <ScrollText size={16} />
              </button>
              {!running(a.status) && a.result_sha256 && !a.team_id && (
                <button
                  className="icon-button"
                  title="Продолжить попытку"
                  aria-label="Продолжить попытку"
                  onClick={() => setResume(a)}
                >
                  <RotateCcw size={16} />
                </button>
              )}
              {a.status === "candidate" &&
                state.entities.find((e) => e.id === a.target)?.formal_goal && (
                  <button
                    className="icon-button"
                    title="Проверить Lean"
                    disabled={live.busy || !live.data.lean?.configured}
                    onClick={() =>
                      act(() =>
                        live.command("/verifications", { attempt: a.id }),
                      )
                    }
                  >
                    <ShieldCheck size={16} />
                  </button>
                )}
              {running(a.status) && (
                <button
                  className="icon-button"
                  title="Остановить попытку"
                  aria-label="Остановить попытку"
                  disabled={live.busy || a.status === "cancelling"}
                  onClick={() => act(() => live.cancel(a.id))}
                >
                  <Square size={16} />
                </button>
              )}
              {a.result_sha256 && (
                <button
                  className="button secondary"
                  disabled={live.busy}
                  onClick={() =>
                    act(async () =>
                      setResult({
                        attempt: a,
                        result: await live.get("/attempts/" + a.id),
                      }),
                    )
                  }
                >
                  <FileText size={15} />
                  Результат
                </button>
              )}
            </div>
          </article>
        ))}
      {result && (
        <section className="attempt-result" aria-label="Результат попытки">
          <div className="section-intro">
            <h2>Результат попытки</h2>
            <button
              className="button secondary"
              onClick={() => setResult(null)}
            >
              Закрыть
            </button>
          </div>
          <div className="metadata">
            <span>Состояние</span>
            <strong>{attemptStates[result.result.status]}</strong>
            <span>Расход токенов</span>
            <strong>
              <UsageSummary usage={result.result.usage} />
            </strong>
            {result.result.failure_code && (
              <>
                <span>Код остановки</span>
                <strong>{result.result.failure_code}</strong>
              </>
            )}
          </div>
          {!!result.result.diagnostics?.length && (
            <section aria-label="Диагностика исполнителя">
              <div className="section-intro">
                <h3>Диагностика исполнителя</h3>
                <button
                  type="button"
                  className="icon-button"
                  title="Скачать диагностику"
                  aria-label="Скачать диагностику"
                  onClick={() => {
                    const a = result.attempt;
                    const r = result.result;
                    const report = {
                      format: "research-team-execution-diagnostics-v1",
                      attempt: {
                        id: a.id,
                        parent_attempt: a.parent_attempt,
                        profile: a.profile,
                        created_at: a.created_at,
                        finished_at: a.finished_at,
                        limits: a.limits,
                      },
                      status: r.status,
                      failure_code: r.failure_code,
                      remote_outcome: r.remote_outcome,
                      profile_sha256: r.profile_sha256,
                      diagnostics: r.diagnostics,
                      usage: r.usage,
                    };
                    const url = URL.createObjectURL(
                      new Blob([JSON.stringify(report, null, 2) + "\n"], {
                        type: "application/json",
                      }),
                    );
                    const link = document.createElement("a");
                    link.href = url;
                    link.download = a.id + ".diagnostics.json";
                    link.click();
                    setTimeout(() => URL.revokeObjectURL(url), 1000);
                  }}
                >
                  <Download size={16} />
                </button>
              </div>
              {result.result.diagnostics.some((d) => d.byte_limit_reached) && (
                <p className="api-error" role="status">
                  Достигнут предел байтов вывода процесса.
                </p>
              )}
              {result.result.diagnostics.map((d, i) => (
                <details
                  key={i}
                  open={d.source !== "process" || d.byte_limit_reached}
                >
                  <summary>
                    {{
                      acp: "Ошибка ACP",
                      stderr: "Поток ошибок",
                      process: "Завершение процесса",
                    }[d.source] || d.source}
                    {" · "}
                    {d.stage}
                    {d.rpc_code != null ? " · " + d.rpc_code : ""}
                  </summary>
                  <pre>{JSON.stringify(d, null, 2)}</pre>
                </details>
              ))}
            </section>
          )}
          <pre>
            {result.result.candidate ||
              result.result.partial ||
              "Кандидат отсутствует."}
          </pre>
          <details>
            <summary>Сведения об исполнении</summary>
            <pre>
              {JSON.stringify(
                { ...result.result, candidate: undefined },
                null,
                2,
              )}
            </pre>
          </details>
          {result.result.status === "candidate" &&
            !result.attempt.team_id &&
            ["proof", "formalize"].includes(
              state.tasks.find((t) => t.id === result.attempt.task_id)?.kind,
            ) && (
              <button
                className="button primary"
                disabled={
                  live.busy ||
                  state.entities.find((e) => e.id === result.attempt.target)
                    ?.revision !== result.attempt.target_revision
                }
                onClick={() =>
                  act(async () => {
                    await live.action({
                      type: "ATTACH_PROOF",
                      target: result.attempt.target,
                      attempt: result.attempt.id,
                    });
                    onSelect(result.attempt.target);
                  })
                }
              >
                <Check size={15} />
                Направить материал на приемку
              </button>
            )}
        </section>
      )}
      {!providers && <TelegramPanel live={live} study={study} />}
      {!providers && <WorkerPanel live={live} study={study} />}
    </div>
  );
}
