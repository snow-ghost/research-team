import React, { useEffect, useRef, useState } from "react";
import {
  GitBranch,
  Plus,
  RefreshCw,
  Play,
  CircleAlert,
  Download,
} from "lucide-react";
import { commandID } from "./api.js";

const commands = {
  observe: "Обновить GitHub",
  submit: "Опубликовать задание",
  "approve-plan": "Подтвердить план",
  reply: "Отправить ответ",
  "request-changes": "Отправить рецензию",
  collect: "Собрать материалы",
  reconcile: "Сверить исход",
  hold: "Запретить новые отправки",
  resume: "Разрешить новые отправки",
};
const phases = {
  plan_available: "План ожидает подтверждения",
  review_plan_available: "План исправлений ожидает подтверждения",
  pull_request_draft: "Черновик запроса на слияние",
  pull_request_open: "Запрос на слияние открыт",
  pull_request_closed: "Запрос на слияние закрыт",
  pull_request_merged: "Изменения включены в основную ветвь",
  issue_closed: "Задача закрыта",
  bot_not_assigned: "Coddy не назначен",
  waiting_for_coddy: "Ожидание Coddy",
};
export function CoddyPanel({ live, onTarget }) {
  const { state, coddy, profiles } = live.data;
  const [selected, setSelected] = useState("");
  const [record, setRecord] = useState(null);
  const [prepare, setPrepare] = useState(false);
  const [kind, setKind] = useState("observe");
  const [error, setError] = useState("");
  const [uncertain, setUncertain] = useState(false);
  const [formEpoch, setFormEpoch] = useState(0);
  const [comments, setComments] = useState([
    { path: "", line: 1, side: "RIGHT", body: "" },
  ]);
  const selectedRef = useRef(selected);
  selectedRef.current = selected;
  const target = state.delegations.find((d) => d.id === selected)?.target;
  useEffect(() => {
    if (target) onTarget(target);
  }, [target, onTarget]);
  async function read(id) {
    const next = await live.get("/coddy/" + id);
    if (selectedRef.current === id) setRecord(next);
    return next;
  }
  useEffect(() => {
    if (!selected && state.delegations.length)
      setSelected(state.delegations.at(-1).id);
  }, [state.delegations, selected]);
  useEffect(() => {
    setRecord(null);
    setError("");
    setUncertain(false);
    if (!selected) return;
    read(selected).catch((e) => setError(e.message));
    const timer = setInterval(() => read(selected).catch(() => {}), 10000);
    return () => clearInterval(timer);
  }, [selected]);
  const plans = record
    ? [
        ...(record.last_observation?.view.issue_comments || []).map((c) => ({
          ...c,
          target: "issue",
        })),
        ...(record.last_observation?.view.pull_comments || []).map((c) => ({
          ...c,
          target: "pr",
        })),
      ].filter((c) => c.user?.login === coddy.bot_login)
    : [];
  const publishing = [
    "submit",
    "approve-plan",
    "reply",
    "request-changes",
  ].includes(kind);
  const publicationBlocked =
    uncertain || record?.operations?.some((op) => op.state === "unknown");
  async function send(values) {
    setError("");
    const [target, comment] = (values.plan || "issue:0").split(":");
    const body = {
      kind,
      request_id: commandID(),
      target: kind === "approve-plan" ? target : values.target || "issue",
      expected_view: record?.last_observation?.digest || "",
      comment_id: Number(comment),
      publish: values.publish === "on",
      body: values.body || "",
      ...(kind === "request-changes"
        ? {
            review: {
              body: values.body,
              comments: comments.map((c) => ({ ...c, line: Number(c.line) })),
            },
          }
        : {}),
    };
    try {
      const next = await live.coddyCommand(selected, body);
      setRecord(next);
      setUncertain(false);
      setFormEpoch((epoch) => epoch + 1);
      await live.refresh();
    } catch (e) {
      setError(e.message);
      if (publishing) {
        setUncertain(true);
        setKind("reconcile");
      }
      await read(selected).catch(() => {});
    }
  }
  return (
    <div className="scroll-content coddy-panel">
      <div className="section-intro">
        <h2>
          <GitBranch size={18} /> Coddy Bot
        </h2>
        <span>{coddy.repository || "Подключение не настроено"}</span>
        <button
          className="button secondary"
          disabled={live.busy || !coddy.available || !state.tasks.length}
          onClick={() => setPrepare(!prepare)}
        >
          <Plus size={15} />
          Подготовить задание
        </button>
      </div>
      {!coddy.available && (
        <p className="api-error">
          <CircleAlert size={16} />
          {coddy.configured
            ? "Учетные данные Coddy недоступны."
            : "Coddy Bot не настроен на сервере."}
        </p>
      )}
      {error && (
        <p role="alert" className="api-error">
          <CircleAlert size={16} />
          {error}
        </p>
      )}
      {prepare && (
        <form
          className="runtime-form"
          onSubmit={async (e) => {
            e.preventDefault();
            const v = Object.fromEntries(new FormData(e.currentTarget));
            setError("");
            try {
              const next = await live.prepareCoddy({
                task_id: v.task_id,
                source_commit: v.source_commit,
                acceptance: v.acceptance
                  .split("\n")
                  .map((x) => x.trim())
                  .filter(Boolean),
                ...(v.profile ? { profile: v.profile } : {}),
              });
              setSelected(next.state.delegations.at(-1).id);
              setPrepare(false);
            } catch (err) {
              setError(err.message);
            }
          }}
        >
          <label>
            Задание
            <select name="task_id" required>
              {state.tasks.map((t) => (
                <option value={t.id} key={t.id}>
                  {t.title}
                </option>
              ))}
            </select>
          </label>
          <label>
            Навыки
            <select name="profile">
              <option value="">Без дополнительного пакета</option>
              {profiles.map((p) => (
                <option value={p.id} key={p.id}>
                  {p.label}
                </option>
              ))}
            </select>
          </label>
          <label className="wide">
            Исходный хеш Git
            <input
              name="source_commit"
              pattern="[a-f0-9]{40}"
              required
              maxLength={40}
            />
          </label>
          <label className="wide">
            Условия приемки
            <textarea name="acceptance" required maxLength={8000} />
          </label>
          <button className="button primary" disabled={live.busy}>
            <Plus size={15} />
            Подготовить без публикации
          </button>
        </form>
      )}
      <div className="delegation-list">
        {state.delegations.map((d) => (
          <button
            key={d.id}
            disabled={live.busy}
            className={
              "delegation-row " + (selected === d.id ? "selected-row" : "")
            }
            onClick={() => setSelected(d.id)}
          >
            <GitBranch size={16} />
            <span>
              {state.entities.find((e) => e.id === d.target)?.title}
              <small>{d.id}</small>
            </span>
          </button>
        ))}
      </div>
      {record && (
        <>
          <div className="section-intro">
            <h2>{record.job.title}</h2>
            <button
              className="icon-button"
              aria-label="Прочитать локальное состояние Coddy"
              title="Прочитать локальное состояние Coddy"
              disabled={live.busy}
              onClick={() => read(selected).catch((e) => setError(e.message))}
            >
              <RefreshCw size={16} />
            </button>
          </div>
          <dl className="metadata">
            <dt>Состояние</dt>
            <dd>{phases[record.last_observation?.phase] ||
              (record.issue_number ? "Задание опубликовано" : "Подготовлено локально")}</dd>
            <dt>Новые отправки</dt>
            <dd>{record.held ? "Запрещены" : "Разрешены"}</dd>
            <dt>Последнее наблюдение</dt>
            <dd>{record.last_observation?.observed_at || "Отсутствует"}</dd>
          </dl>
          {record.issue_number > 0 && (
            <p>
              <a
                target="_blank"
                rel="noreferrer"
                href={
                  "https://github.com/" +
                  coddy.repository +
                  "/issues/" +
                  record.issue_number
                }
              >
                Задача #{record.issue_number}
              </a>
              {record.last_observation?.view.pull && (
                <>
                  {" "}
                  ·{" "}
                  <a
                    target="_blank"
                    rel="noreferrer"
                    href={
                      "https://github.com/" +
                      coddy.repository +
                      "/pull/" +
                      record.last_observation.view.pull.number
                    }
                  >
                    Запрос на слияние #
                    {record.last_observation.view.pull.number}
                  </a>
                </>
              )}
            </p>
          )}
          {publicationBlocked && (
            <p className="api-error" role="alert">
              Исход отправки требует проверки. Новая публикация заблокирована до
              сверки.
            </p>
          )}
          <form
            className="runtime-form"
            key={selected + kind + formEpoch + record.last_observation?.digest}
            onSubmit={(e) => {
              e.preventDefault();
              send(Object.fromEntries(new FormData(e.currentTarget)));
            }}
          >
            <label>
              Действие
              <select value={kind} onChange={(e) => setKind(e.target.value)}>
                {Object.entries(commands).map(([id, title]) => (
                  <option
                    value={id}
                    key={id}
                    disabled={
                      publicationBlocked &&
                      !["reconcile", "observe", "hold"].includes(id)
                    }
                  >
                    {title}
                  </option>
                ))}
              </select>
            </label>
            {kind === "approve-plan" && (
              <label>
                Сообщение Coddy
                <select name="plan" required>
                  <option value="">Выберите план</option>
                  {plans.map((p) => (
                    <option key={p.target + p.id} value={p.target + ":" + p.id}>
                      {p.target} · #{p.id}: {p.body.slice(0, 80)}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {kind === "reply" && (
              <label>
                Назначение
                <select name="target">
                  <option value="issue">Задача</option>
                  <option value="pr">Запрос на слияние</option>
                </select>
              </label>
            )}
            {["reply", "request-changes"].includes(kind) && (
              <label className="wide">
                Текст
                <textarea name="body" required maxLength={8000} />
              </label>
            )}
            {kind === "request-changes" && (
              <div className="wide line-comments">
                {comments.map((c, i) => (
                  <div className="runtime-form" key={i}>
                    <label>
                      Файл
                      <input
                        required
                        value={c.path}
                        onChange={(e) =>
                          setComments(
                            comments.map((x, n) =>
                              n === i ? { ...x, path: e.target.value } : x,
                            ),
                          )
                        }
                      />
                    </label>
                    <label>
                      Новая строка
                      <input
                        type="number"
                        min="1"
                        required
                        value={c.line}
                        onChange={(e) =>
                          setComments(
                            comments.map((x, n) =>
                              n === i ? { ...x, line: e.target.value } : x,
                            ),
                          )
                        }
                      />
                    </label>
                    <label className="wide">
                      Замечание
                      <textarea
                        required
                        value={c.body}
                        onChange={(e) =>
                          setComments(
                            comments.map((x, n) =>
                              n === i ? { ...x, body: e.target.value } : x,
                            ),
                          )
                        }
                      />
                    </label>
                  </div>
                ))}
                <button
                  type="button"
                  className="button secondary"
                  onClick={() =>
                    setComments([
                      ...comments,
                      { path: "", line: 1, side: "RIGHT", body: "" },
                    ])
                  }
                >
                  <Plus size={15} />
                  Добавить замечание
                </button>
              </div>
            )}
            {publishing && (
              <label className="checkbox wide">
                <input type="checkbox" name="publish" required />
                {kind === "approve-plan"
                  ? "Разрешаю Coddy выполнять план, изменять код и публиковать изменения"
                  : "Подтверждаю отправку данных в GitHub и возможный запуск Coddy"}
              </label>
            )}
            <button
              className="button primary"
              disabled={
                live.busy ||
                !coddy.available ||
                (publicationBlocked && publishing)
              }
            >
              <Play size={15} />
              {commands[kind]}
            </button>
          </form>
          <h3>Сообщения Coddy</h3>
          {plans.map((p) => (
            <article className="attempt-row" key={p.target + p.id}>
              <div>
                <strong>
                  {p.target} · #{p.id}
                </strong>
                <pre>{p.body}</pre>
              </div>
            </article>
          ))}
          <h3>Публикации</h3>
          {(record.operations || []).map((op) => (
            <p key={op.request_id}>
              <code>{op.kind}</code> · {op.state} · <code>{op.request_id}</code>
            </p>
          ))}
          <h3>Материалы кандидата</h3>
          {(record.candidate_sha256 || []).map((sha) => (
            <button
              key={sha}
              className="button secondary"
              onClick={async () => {
                try {
                  const value = await live.get(
                    "/coddy/" + selected + "/candidates/" + sha,
                  );
                  const url = URL.createObjectURL(
                    new Blob([JSON.stringify(value, null, 2)], {
                      type: "application/json",
                    }),
                  );
                  const a = document.createElement("a");
                  a.href = url;
                  a.download = "coddy-candidate-" + sha + ".json";
                  a.click();
                  setTimeout(() => URL.revokeObjectURL(url), 1000);
                } catch (e) {
                  setError(e.message);
                }
              }}
            >
              <Download size={15} />
              {sha.slice(0, 16)}
            </button>
          ))}
        </>
      )}
    </div>
  );
}
