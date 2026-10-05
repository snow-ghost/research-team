import React, { useState } from "react";
import { Play, Pause, Square, Plus, BookOpen, CircleAlert } from "lucide-react";

export const cycleStates = {
  running: "В работе",
  awaiting_review: "Ожидает приемки",
  paused: "Приостановлен",
  blocked: "Требует решения",
  stopped: "Остановлен",
  completed: "Цель принята",
  exhausted: "Исчерпаны попытки",
  failed: "Ошибка исполнения",
  interrupted: "Прерван при перезапуске",
};

export function CyclePanel({ live, study, all, onSelect }) {
  const { state, profiles, workspaces } = live.data;
  const [creating, setCreating] = useState(false);
  const [resuming, setResuming] = useState("");
  const [error, setError] = useState("");
  const cycles = (state.cycles || []).filter((c) => all || c.study === study);
  async function act(callback) {
    setError("");
    try {
      await callback();
    } catch (e) {
      setError(e.message);
    }
  }
  return (
    <section className="cycle-panel" aria-label="Исследовательские циклы">
      <div className="section-intro">
        <h2>Исследовательские циклы</h2>
        <button
          className="button secondary"
          disabled={live.busy || !state.studies.length}
          onClick={() => setCreating(!creating)}
        >
          <Plus size={15} /> Новый цикл
        </button>
      </div>
      {error && (
        <p role="alert" className="api-error">
          <CircleAlert size={16} />
          {error}
        </p>
      )}
      {creating && (
        <form
          className="runtime-form"
          aria-label="Новый исследовательский цикл"
          onSubmit={(event) => {
            event.preventDefault();
            const v = Object.fromEntries(new FormData(event.currentTarget));
            act(async () => {
              await live.startCycle({
                study: v.study,
                profile: v.profile,
                workspace: v.workspace,
                max_attempts: Number(v.max_attempts),
                confirm: v.confirm === "on",
              });
              setCreating(false);
            });
          }}
        >
          <label>
            Исследование
            <select name="study" defaultValue={study} required>
              {state.studies.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.title}
                </option>
              ))}
            </select>
          </label>
          <label>
            Исполнитель
            <select
              name="profile"
              defaultValue={profiles.find((p) => p.available)?.id || ""}
              required
            >
              <option value="" disabled>
                Выберите профиль
              </option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id} disabled={!p.available}>
                  {p.label}
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
          <label>
            Предел попыток
            <input
              name="max_attempts"
              type="number"
              min="1"
              max="20"
              step="1"
              defaultValue="4"
              required
            />
          </label>
          <label className="checkbox wide">
            <input name="confirm" type="checkbox" required />
            Разрешаю автоматические запуски в пределах числа попыток и возможные
            расходы модели
          </label>
          <button
            className="button primary"
            disabled={
              live.busy ||
              state.paused ||
              !profiles.some((p) => p.available) ||
              !workspaces.length
            }
          >
            <Play size={15} /> Начать цикл
          </button>
        </form>
      )}
      {!cycles.length && <p className="muted">Циклов пока нет</p>}
      {cycles
        .slice()
        .reverse()
        .map((c) => {
          const attempt = state.attempts.find(
            (a) => a.id === c.current_attempt,
          );
          return (
            <article key={c.id} className="cycle-row" data-cycle-id={c.id}>
              <div className="cycle-heading">
                <strong>
                  {state.studies.find((s) => s.id === c.study)?.title}
                </strong>
                <span>{cycleStates[c.status] || c.status}</span>
              </div>
              <p>{c.reason}</p>
              <small>
                {c.used_attempts} / {c.max_attempts} попыток · {c.profile}
              </small>
              {c.accepted_snapshot && (
                <small>
                  Приемка зафиксирована в снимке v{c.accepted_snapshot}
                </small>
              )}
              <div className="row-actions">
                <button
                  className="button secondary"
                  onClick={() => onSelect(attempt?.target || c.goal)}
                >
                  <BookOpen size={15} /> Открыть утверждение
                </button>
                {["running", "awaiting_review"].includes(c.status) && (
                  <button
                    className="icon-button"
                    title="Приостановить цикл"
                    aria-label="Приостановить цикл"
                    disabled={live.busy}
                    onClick={() =>
                      act(() => live.controlCycle(c.id, { kind: "pause" }))
                    }
                  >
                    <Pause size={16} />
                  </button>
                )}
                {["paused", "blocked"].includes(c.status) && (
                  <button
                    className="icon-button"
                    title="Продолжить цикл"
                    aria-label="Продолжить цикл"
                    disabled={live.busy}
                    onClick={() => setResuming(c.id)}
                  >
                    <Play size={16} />
                  </button>
                )}
                {["running", "awaiting_review", "paused", "blocked"].includes(
                  c.status,
                ) && (
                  <button
                    className="icon-button"
                    title="Остановить цикл"
                    aria-label="Остановить цикл"
                    disabled={live.busy}
                    onClick={() =>
                      act(() => live.controlCycle(c.id, { kind: "stop" }))
                    }
                  >
                    <Square size={16} />
                  </button>
                )}
              </div>
              {resuming === c.id &&
                ["paused", "blocked"].includes(c.status) && (
                  <form
                    className="runtime-form"
                    onSubmit={(event) => {
                      event.preventDefault();
                      const confirm =
                        new FormData(event.currentTarget).get("confirm") ===
                        "on";
                      act(async () => {
                        await live.controlCycle(c.id, {
                          kind: "resume",
                          confirm,
                        });
                        setResuming("");
                      });
                    }}
                  >
                    <label className="checkbox wide">
                      <input name="confirm" type="checkbox" required />
                      Подтверждаю продолжение и возможные расходы оставшихся
                      попыток
                    </label>
                    <button className="button primary" disabled={live.busy}>
                      <Play size={15} /> Продолжить
                    </button>
                  </form>
                )}
            </article>
          );
        })}
    </section>
  );
}
