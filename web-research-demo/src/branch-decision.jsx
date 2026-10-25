import React, { useState } from "react";
import { GitBranch } from "lucide-react";
import { BranchPolicyForm } from "./branch-policy.jsx";

const kinds = {
  continue: "Продолжение",
  change_method: "Смена приема",
  split: "Разбиение",
  stop: "Остановка",
};
const outcomes = {
  continued: "Продолжена",
  replacement_created: "Создана новая ветвь",
  awaiting_separate_plan: "Ожидается отдельный план",
  stopped: "Остановлена",
};

export function BranchDecisionForm({ live, branch, methods }) {
  const [kind, setKind] = useState("split"),
    [error, setError] = useState("");
  const terminal = [
    "completed",
    "invalidated",
    "abandoned",
    "cancelled",
  ].includes(branch.status);
  const entity = live.data.state.entities.find((e) => e.id === branch.target);
  return (
    <div className="branch-decisions">
      <BranchPolicyForm live={live} branch={branch} methods={methods} />
      {!!branch.decisions?.length && (
        <details>
          <summary>Принятые решения: {branch.decisions.length}</summary>
          {branch.decisions.map((d) => (
            <div className="decision-entry" key={d.id}>
              <strong>
                {kinds[d.kind] || d.kind} · {outcomes[d.outcome] || d.outcome}
              </strong>
              <p>{d.note}</p>
              <small>
                Версия цели {d.target_revision}. {d.created_at}
              </small>
              {d.evidence_attempt && (
                <p>
                  Основание: <code>{d.evidence_attempt}</code>
                </p>
              )}
              {d.replacement && (
                <p>
                  Новая ветвь: <code>{d.replacement}</code>
                </p>
              )}
            </div>
          ))}
        </details>
      )}
      {!terminal && (
        <details>
          <summary>Решение по ветви</summary>
          <form
            className="runtime-form"
            aria-label={`Решение по ветви ${branch.id}`}
            onSubmit={async (e) => {
              e.preventDefault();
              setError("");
              const f = Object.fromEntries(new FormData(e.currentTarget));
              try {
                await live.command(`/branches/${branch.id}/decisions`, {
                  kind,
                  method: f.method || "",
                  note: f.note,
                  note_kind: f.note_kind,
                  evidence_attempt: f.evidence_attempt || "",
                  confirm: true,
                });
              } catch (err) {
                setError(err.message);
              }
            }}
          >
            <label>
              Действие
              <select value={kind} onChange={(e) => setKind(e.target.value)}>
                {Object.entries(kinds).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
            {kind === "change_method" && (
              <label>
                Новый прием
                <select name="method" required defaultValue="">
                  <option value="" disabled>
                    Выберите прием
                  </option>
                  {methods
                    .filter((m) => m.id !== branch.method)
                    .map((m) => (
                      <option key={m.id} value={m.id}>
                        {m.label}
                      </option>
                    ))}
                </select>
              </label>
            )}
            <label>
              Вид основания
              <select name="note_kind" defaultValue="unspecified">
                <option value="unspecified">Не указан</option>
                <option value="operational">Организационное решение</option>
                <option value="mathematical_hint">
                  Математическая подсказка
                </option>
              </select>
            </label>
            <label className="wide">
              Завершенная попытка
              <select name="evidence_attempt" defaultValue="">
                <option value="">Без ссылки на попытку</option>
                {live.data.state.attempts
                  .filter(
                    (a) =>
                      a.target === branch.target &&
                      a.target_revision === entity?.revision &&
                      ![
                        "queued",
                        "preparing",
                        "running",
                        "cancelling",
                        "awaiting_worker",
                      ].includes(a.status),
                  )
                  .map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.id} · {a.status}
                    </option>
                  ))}
              </select>
            </label>
            <label className="wide">
              Основание решения
              <textarea name="note" maxLength={4000} required />
            </label>
            <label className="checkbox-label wide">
              <input type="checkbox" required />
              Подтверждаю решение и разрешенные расходы при продолжении
            </label>
            <button className="button secondary" disabled={live.busy}>
              <GitBranch size={15} />
              Записать решение
            </button>
          </form>
        </details>
      )}
      {error && (
        <p className="api-error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
