import React, { useEffect, useState } from "react";
import { Check, ShieldCheck } from "lucide-react";
import { api } from "./api.js";

export function ProviderControlPanel({ live }) {
  const [error, setError] = useState("");
  const controls = live.data.state.provider_controls || [];
  const groups = [
    ...new Map(
      live.data.profiles.map((p) => [p.provider_key || p.id, p]),
    ).values(),
  ];
  return (
    <section aria-label="Допуск поставщиков">
      <div className="section-intro">
        <h2>Допуск поставщиков</h2>
        <span>{controls.filter((c) => c.held).length} удержаний</span>
      </div>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Модель</th>
              <th>Передачи</th>
              <th>Последовательные отказы</th>
              <th>Последняя запись</th>
            </tr>
          </thead>
          <tbody>
            {groups.map((p) => {
              const c = controls.find(
                (c) => c.key === (p.provider_key || p.id),
              );
              return (
                <tr key={p.provider_key || p.id}>
                  <td>
                    {p.model || p.label}
                    <small>{p.id}</small>
                  </td>
                  <td>{c?.held ? "Удержаны" : "Разрешены"}</td>
                  <td>{c?.consecutive_failures || 0}</td>
                  <td>{c?.note || "Нет записи"}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <details>
        <summary>Изменить допуск</summary>
        <form
          className="runtime-form"
          onSubmit={async (e) => {
            e.preventDefault();
            setError("");
            const f = Object.fromEntries(new FormData(e.currentTarget));
            try {
              await live.command("/providers/commands", {
                profile: f.profile,
                kind: f.kind,
                note: f.note,
                confirm: f.confirm === "on",
              });
            } catch (e) {
              setError(e.message);
            }
          }}
        >
          <label>
            Профиль
            <select name="profile">
              {live.data.profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            Действие
            <select name="kind">
              <option value="note">Записать наблюдение</option>
              <option value="pause">Удержать передачи</option>
              <option value="resume">Разрешить передачи</option>
            </select>
          </label>
          <label className="wide">
            Основание
            <textarea name="note" required maxLength={4000} />
          </label>
          <label className="checkbox-label wide">
            <input name="confirm" type="checkbox" required />
            Подтверждаю изменение или запись для общего адреса поставщика
          </label>
          <button className="button secondary" disabled={live.busy}>
            <Check size={15} />
            Применить
          </button>
        </form>
      </details>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
    </section>
  );
}

export function CounterReportPanel({ live, attempt }) {
  const [value, setValue] = useState(null),
    [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    api(`/attempts/${attempt.id}/counter-report`)
      .then((v) => {
        if (active) {
          setValue(v);
          setError("");
        }
      })
      .catch((e) => {
        if (active) setError(e.message);
      });
    return () => {
      active = false;
    };
  }, [attempt.id, live.data.state.revision]);
  return (
    <section aria-label="Разбор отчета контрпримеров">
      <div className="section-intro">
        <h3>Отчет контрпримеров</h3>
        <span>
          {value?.cleared ? "Вопросы разобраны" : "Требуется проверка"}
        </span>
      </div>
      {value && (
        <>
          <p>{value.report.evidence}</p>
          {value.report.assumptions && (
            <details>
              <summary>Предпосылки</summary>
              <p>{value.report.assumptions}</p>
            </details>
          )}
          {value.report.limitations && (
            <details>
              <summary>Ограничения</summary>
              <p>{value.report.limitations}</p>
            </details>
          )}
          {(value.issues || []).map((i, n) => (
            <p key={n}>
              <strong>
                {i.kind === "mathematical"
                  ? "Математический вопрос"
                  : i.kind === "operational"
                    ? "Операционный вопрос"
                    : "Вопрос без классификации"}
                :
              </strong>{" "}
              {i.text}
            </p>
          ))}
          {value.auditable && !value.cleared && (
            <form
              className="runtime-form"
              onSubmit={async (e) => {
                e.preventDefault();
                setError("");
                const f = Object.fromEntries(new FormData(e.currentTarget));
                try {
                  await live.command("/counter-audits", {
                    attempt: attempt.id,
                    report_sha256: value.report_sha256,
                    note: f.note,
                    confirm: f.confirm === "on",
                  });
                } catch (e) {
                  setError(e.message);
                }
              }}
            >
              <label className="wide">
                Основание классификации
                <textarea name="note" required maxLength={4000} />
              </label>
              <label className="checkbox-label wide">
                <input type="checkbox" name="confirm" required />
                Подтверждаю, что перечисленные вопросы относятся только к
                организации работы
              </label>
              <button className="button secondary" disabled={live.busy}>
                <ShieldCheck size={15} />
                Записать разбор
              </button>
            </form>
          )}
        </>
      )}
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
    </section>
  );
}
