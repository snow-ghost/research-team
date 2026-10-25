import React from "react";

const modes = {
  single: "Один исследователь",
  team: "Фиксированная команда",
  demand: "По необходимости",
  adaptive: "Адаптивная команда",
  lean: "Контроль Lean",
};
const classes = {
  polynomials: "Полиномы",
  divisibility: "Делимость",
  finite_sets: "Конечные множества",
  logic: "Логика",
  functions: "Функции",
  finite_sums: "Конечные суммы",
};
const statuses = {
  candidate: "Получен ответ",
  invalid_report: "Неверная структура ответа",
  failed: "Ошибка исполнения",
  timed_out: "Время исчерпано",
  limit_reached: "Предел исчерпан",
  not_recorded: "Не записана",
  verified_binding: "Сумма файла подтверждена",
  bound_invalid_contract: "Сумма подтверждена, структура неверна",
  unavailable: "Файл недоступен или сумма не совпала",
  not_requested: "Местный файл не запрашивался",
  question: "Вопрос",
  major: "Существенное замечание",
  editorial: "Редакционное замечание",
  premise: "Предпосылка",
};
export function ComparisonAnalysis({ value }) {
  if (!value) return null;
  return (
    <section className="comparison-analysis" aria-label="Разбор сравнения">
      <h3>
        {value.preliminary ? "Предварительные данные" : "Завершенный набор"}
      </h3>
      <p>
        Задач с полными парными группами: {value.independent_tasks}. Парных
        повторений: {value.repeat_observations}.
      </p>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Класс</th>
              <th>Режим</th>
              <th>Завершено</th>
              <th>Lean</th>
              <th>Рецензия</th>
              <th>Учтено запросов</th>
              <th>Время, с</th>
            </tr>
          </thead>
          <tbody>
            {Object.entries(value.classes).flatMap(([name, group]) =>
              Object.entries(group.paired).map(([mode, row]) => (
                <tr key={name + mode}>
                  <td>{classes[name] || name}</td>
                  <td>{modes[mode] || mode}</td>
                  <td>{row.runs}</td>
                  <td>{row.native_verified}</td>
                  <td>{row.reviewed}</td>
                  <td>{row.charged_requests}</td>
                  <td>{row.seconds.toFixed(1)}</td>
                </tr>
              )),
            )}
          </tbody>
        </table>
      </div>
      <details>
        <summary>Различия по задачам: {value.task_effects.length}</summary>
        {value.task_effects.map((row) => (
          <p key={row.case + row.mode}>
            <code>{row.case}</code> · {modes[row.mode] || row.mode}: разница
            доли результатов с рецензией{" "}
            {row.reviewed_success_difference.toFixed(2)}, запросов{" "}
            {row.request_difference.toFixed(1)}, времени{" "}
            {row.seconds_difference.toFixed(1)} с; пар {row.pairs}.
          </p>
        ))}
      </details>
      <details>
        <summary>
          Lean без завершенной рецензии: {value.review_gaps.length}
        </summary>
        {value.review_gaps.map((row) => (
          <div
            className="decision-entry"
            key={row.case + row.mode + row.repetition}
          >
            <strong>
              {row.case} · {modes[row.mode] || row.mode} · повтор{" "}
              {row.repetition}
            </strong>
            <p>
              Рецензия: {statuses[row.review_status] || row.review_status}.
              Связь файла:{" "}
              {statuses[row.artifact_status] || row.artifact_status}.
            </p>
            {row.findings?.map((f, i) => (
              <p key={i}>
                {statuses[f.severity] || f.severity} ·{" "}
                {statuses[f.category] || f.category}: {f.text}
                <br />
                {f.evidence}
                <br />
                {f.location}
              </p>
            ))}
          </div>
        ))}
      </details>
    </section>
  );
}
