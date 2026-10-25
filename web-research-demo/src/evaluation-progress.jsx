import React, { useEffect, useState } from "react";
import { Activity } from "lucide-react";

const modeNames = {
  single: "Один исследователь",
  team: "Фиксированная команда",
  demand: "Специалисты по необходимости",
  lean: "Контроль Lean",
};
const stateNames = {
  running: "В работе",
  completed: "Завершено",
  not_started: "Не начато",
  unavailable: "Отчет недоступен",
  invalid: "Отчет не прошел проверку",
  failed: "Ошибка",
  inconclusive: "Без результата",
  requires_review: "Нужна рецензия",
  verified_not_accepted: "Проверено, не принято",
  verified_baseline_not_accepted: "Проверено Lean, не принято",
};

export function EvaluationProgress({ live }) {
  const [items, setItems] = useState([]),
    [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    const poll = async () => {
      try {
        const data = await live.get("/evaluations");
        if (active) {
          setItems(data);
          setError("");
        }
      } catch (e) {
        if (active) setError(e.message);
      }
    };
    poll();
    const timer = setInterval(poll, 5000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [live.get]);
  if (!items.length && !error) return null;
  return (
    <section className="process-section" aria-label="Выполняющиеся сравнения">
      <h3>
        <Activity size={16} />
        Сравнение исследователя и команды
      </h3>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {items.map((item) => (
        <div className="evaluation-row" key={item.id}>
          <div className="section-intro">
            <strong>{item.label}</strong>
            <span>{stateNames[item.status] || item.status}</span>
          </div>
          {item.summary && (
            <>
              <p>
                Завершено запусков: {item.summary.terminal_runs} /{" "}
                {item.summary.expected_runs}. Полных парных групп:{" "}
                {item.summary.paired_groups}.
              </p>
              <p>
                Зарегистрировано обращений: {item.measured_requests}. Учтенные
                обращения и резервы: {item.request_upper_bound} /{" "}
                {item.max_requests}.
              </p>
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Режим</th>
                      <th>Завершено</th>
                      <th>Lean</th>
                      <th>Рецензия</th>
                      <th>Ошибки</th>
                      <th>Неполный расход</th>
                    </tr>
                  </thead>
                  <tbody>
                    {Object.entries(item.summary.all_recorded).map(
                      ([mode, row]) => (
                        <tr key={mode}>
                          <td>{modeNames[mode] || mode}</td>
                          <td>{row.runs}</td>
                          <td>{row.native_verified}</td>
                          <td>{row.reviewed}</td>
                          <td>{row.failed}</td>
                          <td>{row.incomplete_usage}</td>
                        </tr>
                      ),
                    )}
                  </tbody>
                </table>
              </div>
              {item.runs
                .filter((r) => r.status === "running")
                .map((r) => (
                  <p key={`${r.case}:${r.mode}:${r.repetition}`}>
                    В работе: <code>{r.case}</code>,{" "}
                    {modeNames[r.mode] || r.mode}, повтор {r.repetition}.
                  </p>
                ))}
              <p>Математическая приемка: не выполнена.</p>
            </>
          )}
          {item.updated_at && (
            <small>
              Отчет обновлен:{" "}
              {new Date(item.updated_at).toLocaleString("ru-RU")}
            </small>
          )}
        </div>
      ))}
    </section>
  );
}
