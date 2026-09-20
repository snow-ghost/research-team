import React, { useEffect, useState } from "react";
import {
  Play,
  Square,
  Plus,
  FileText,
  Check,
  CircleAlert,
  Cpu,
  RefreshCw,
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
};
const running = (status) =>
  ["queued", "preparing", "running", "cancelling"].includes(status);

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
            <select name="kind">
              <option value="proof">Поиск доказательства</option>
              <option value="review">Рецензия</option>
              <option value="counterexample">Поиск контрпримера</option>
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
                confirm: values.confirm === "on",
              }),
            );
          }}
        >
          <label>
            Исполнитель
            <select
              name="profile"
              required
              defaultValue={profiles.find((p) => p.available)?.id || ""}
            >
              <option value="" disabled>
                Выберите профиль
              </option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id} disabled={!p.available}>
                  {p.label}
                  {p.available ? "" : " (недоступен)"}
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
          <button
            className="button primary"
            disabled={
              live.busy ||
              !profiles.some((p) => p.available) ||
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
              {result.result.usage
                ? JSON.stringify(result.result.usage)
                : "Неизвестен"}
            </strong>
          </div>
          <pre>{result.result.candidate || "Кандидат отсутствует."}</pre>
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
            state.tasks.find((t) => t.id === result.attempt.task_id)?.kind ===
              "proof" && (
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
    </div>
  );
}
