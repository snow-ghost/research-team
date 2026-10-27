import React, { useEffect, useRef, useState } from "react";
import {
  FileCode2,
  ClipboardCheck,
  GitBranch,
  ChartNoAxesCombined,
  Download,
  CircleCheck,
  CircleAlert,
  Clock3,
  ArrowRight,
} from "lucide-react";

const labels = {
  verified: "Проверено",
  ready: "Готово к решению",
  accepted: "Принято",
  refuted: "Опровергнуто",
  in_review: "На рассмотрении",
  open: "Открыто",
  pending: "Ожидается",
  not_recorded: "Нет свидетельства о файлах",
  needs_changes: "Нужны исправления",
  stale: "Устарело",
  none_found: "Контрпример не найден",
  counterexample_candidate: "Кандидат контрпримера",
  inconclusive: "Проверка не завершена",
  failed: "Ошибка",
  interrupted: "Прервано",
  cancelled: "Отменено",
  preparing: "Подготовка",
  awaiting_worker: "Ожидается исполнитель",
  cancelling: "Отмена",
  queued: "В очереди",
  running: "Выполняется",
  candidate: "Кандидат",
  timed_out: "Истек срок",
  limit_reached: "Исчерпан предел",
  invalid_format: "Ошибка формата",
  unknown: "Исход неизвестен",
  response_received: "Ответ получен",
  not_started: "Не отправлено",
  dns_timeout: "Срок определения имени",
  connect_timeout: "Срок соединения",
  tls_timeout: "Срок защищенного соединения",
  dns_failure: "Ошибка определения имени",
  connect_failure: "Ошибка соединения",
  tls_failure: "Ошибка защищенного соединения",
  output_limit: "Предел вывода",
  tool_limit: "Предел инструментов или шагов",
  report_format: "Формат отчета",
  availability: "Доступность",
  configuration: "Настройка",
  other: "Прочее",
  proof: "Доказательство",
  counterexample: "Контрпримеры",
  review: "Рецензия",
  formalize: "Формализация",
};
const text = (value) => labels[value] || value || "Нет данных";
const number = (value) => (value || 0).toLocaleString("ru-RU");
const modes = [
  ["files", "Файлы", FileCode2],
  ["checks", "Проверки", ClipboardCheck],
  ["dependencies", "Зависимости", GitBranch],
  ["usage", "Расход", ChartNoAxesCombined],
];

function saveFile(name, source) {
  const url = URL.createObjectURL(
    new Blob([source], { type: "text/plain;charset=utf-8" }),
  );
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  URL.revokeObjectURL(url);
}

export function ProofInspectionPanel({ live, study, target, onSelect }) {
  const [data, setData] = useState(null),
    [error, setError] = useState(""),
    [mode, setMode] = useState("files");
  const get = useRef(live.get);
  get.current = live.get;
  useEffect(() => {
    let current = true;
    setData((previous) => (previous?.target.id === target ? previous : null));
    setError("");
    if (target)
      get
        .current(`/entities/${encodeURIComponent(target)}/inspection`)
        .then((value) => {
          if (current) setData(value);
        })
        .catch((e) => {
          if (current) setError(e.message);
        });
    return () => {
      current = false;
    };
  }, [target, live.data.state.revision]);
  const files = data && [
    {
      name: "Goal.lean",
      title: data.checked_files
        ? "Фактический модуль определений"
        : "Постановка из записи",
      source:
        data.checked_files?.definitions.source ||
        data.verification?.goal.source ||
        data.target.formal_goal?.source,
      sha: data.checked_files?.definitions.raw_sha256,
    },
    {
      name: "Candidate.lean",
      title: data.checked_files
        ? "Фактический исходник доказательства"
        : "Кандидат из записи",
      source: data.checked_files?.candidate.source || data.verification?.source,
      sha: data.checked_files?.candidate.raw_sha256,
    },
  ];
  return (
    <section className="proof-inspection" aria-label="Проверка утверждения">
      <div className="section-intro">
        <h3>Проверка утверждения</h3>
        {data && <span>Снимок {data.snapshot}</span>}
      </div>
      <label className="inspection-target">
        Утверждение
        <select
          aria-label="Утверждение для проверки"
          value={target || ""}
          onChange={(e) => onSelect?.(e.target.value, { openInspector: false })}
        >
          {live.data.state.entities
            .filter((e) => e.study === study)
            .map((e) => (
              <option key={e.id} value={e.id}>
                {e.title}
              </option>
            ))}
        </select>
      </label>
      {error && (
        <p role="alert" className="api-error">
          {error}
        </p>
      )}
      {data && (
        <>
          {!data.execution.operational && (
            <p role="alert" className="api-error">
              Обработка остановлена:{" "}
              {data.execution.current_stop?.stage || "этап не записан"};{" "}
              {data.execution.current_stop?.class || "класс не записан"}.
            </p>
          )}
          {data.claim_kind === "refutation" && (
            <p className="inspection-notice">
              Проверяемое утверждение: отрицание исходной цели.
            </p>
          )}
          <ol className="proof-gates" aria-label="Состояние проверок">
            {data.gates.map((gate) => {
              const success = [
                  "verified",
                  "accepted",
                  "ready",
                  "none_found",
                  "refuted",
                ].includes(gate.status),
                failed = ["failed", "needs_changes", "stale"].includes(
                  gate.status,
                );
              const Icon = success
                ? CircleCheck
                : failed
                  ? CircleAlert
                  : Clock3;
              return (
                <li
                  key={gate.code}
                  className={
                    failed ? "gate-failed" : success ? "gate-passed" : ""
                  }
                >
                  <Icon size={16} />
                  <span>{gate.label}</span>
                  <strong>{text(gate.status)}</strong>
                </li>
              );
            })}
          </ol>
          <div
            className="inspection-tabs"
            role="tablist"
            aria-label="Материалы проверки"
          >
            {modes.map(([id, title, Icon]) => (
              <button
                key={id}
                role="tab"
                aria-selected={mode === id}
                aria-controls={`inspection-panel-${id}`}
                id={`inspection-tab-${id}`}
                onClick={() => setMode(id)}
              >
                <Icon size={15} />
                {title}
              </button>
            ))}
          </div>
          <div
            role="tabpanel"
            id={`inspection-panel-${mode}`}
            aria-labelledby={`inspection-tab-${mode}`}
          >
            {mode === "files" && (
              <>
                <h4>Постановка</h4>
                <p>{data.target.statement}</p>
                <h4>Предпосылки</h4>
                <p>{data.target.assumptions}</p>
                {data.file_error && (
                  <p role="alert" className="api-error">
                    {data.file_error}
                  </p>
                )}
                <div className="inspection-file-grid">
                  {files.map((file) => (
                    <section key={file.name} aria-label={file.title}>
                      <div className="inspection-file-heading">
                        <div>
                          <h4>{file.name}</h4>
                          <span>{file.title}</span>
                        </div>
                        <button
                          className="icon-button"
                          title={`Скачать ${file.name}`}
                          aria-label={`Скачать ${file.name}`}
                          disabled={!file.source}
                          onClick={() => saveFile(file.name, file.source)}
                        >
                          <Download size={16} />
                        </button>
                      </div>
                      {file.sha && (
                        <code className="inspection-digest">
                          SHA256 байтов: {file.sha}
                        </code>
                      )}
                      <pre>{file.source || "Исходник не предоставлен"}</pre>
                    </section>
                  ))}
                </div>
              </>
            )}
            {mode === "checks" && (
              <>
                <h4>Замечания и их состояние</h4>
                {!data.findings.length && <p>Замечаний в журнале нет.</p>}
                {data.findings.map((f) => (
                  <details
                    key={f.id}
                    className={`inspection-finding ${f.state !== "resolved" && f.severity !== "editorial" ? "finding-blocking" : ""}`}
                  >
                    <summary>
                      <strong>
                        {f.severity === "major"
                          ? "Ошибка"
                          : f.severity === "question"
                            ? "Вопрос"
                            : "Редакционное"}
                      </strong>
                      <span>
                        {f.state === "resolved"
                          ? "Закрыто"
                          : f.state === "needs_recheck"
                            ? "Перепроверка"
                            : "Открыто"}
                      </span>
                    </summary>
                    <p>{f.text}</p>
                    {f.evidence && <p>{f.evidence}</p>}
                    <code>{f.review_attempt}</code>
                  </details>
                ))}
                <h4>Рецензии точного исходника</h4>
                {data.reviews.map((r) => (
                  <details key={r.attempt} className="inspection-review">
                    <summary>
                      <code>{r.attempt}</code>
                      <span>{text(r.status)}</span>
                    </summary>
                    <p>
                      {r.report?.summary || "Отчет рецензии не предоставлен"}
                    </p>
                    {r.report?.findings.map((f, index) => (
                      <p key={index}>
                        <strong>
                          {f.severity === "editorial"
                            ? "Редакционное"
                            : f.severity === "major"
                              ? "Ошибка"
                              : "Вопрос"}
                          :{" "}
                        </strong>
                        {f.text}
                      </p>
                    ))}
                  </details>
                ))}
                <details>
                  <summary>Отчет Lean</summary>
                  <pre>
                    {JSON.stringify(data.verification?.report || {}, null, 2)}
                  </pre>
                </details>
                <details>
                  <summary>Отчет контрпримеров</summary>
                  <pre>
                    {JSON.stringify(data.result?.counter || {}, null, 2)}
                  </pre>
                </details>
              </>
            )}
            {mode === "dependencies" && (
              <>
                {!data.dependencies.length && (
                  <p>Исследовательские зависимости не назначены.</p>
                )}
                {data.dependencies.map((d) => (
                  <div className="inspection-dependency" key={d.id}>
                    <div>
                      <strong>{d.title}</strong>
                      <code>
                        {d.id}, версия {d.revision}
                      </code>
                    </div>
                    <span>
                      {text(d.status)}; модуль{" "}
                      {d.library_ready ? "готов" : "не подготовлен"}
                    </span>
                    <button
                      className="icon-button"
                      title={`Открыть ${d.title}`}
                      aria-label={`Открыть ${d.title}`}
                      onClick={() => onSelect?.(d.id, { openInspector: false })}
                    >
                      <ArrowRight size={16} />
                    </button>
                  </div>
                ))}
                <details>
                  <summary>Ссылки в проверенном терме</summary>
                  <pre>
                    {JSON.stringify(
                      data.verification?.report?.dependencies || {},
                      null,
                      2,
                    )}
                  </pre>
                </details>
              </>
            )}
            {mode === "usage" && (
              <>
                <dl className="inspection-budget">
                  <div>
                    <dt>Учтенные обращения</dt>
                    <dd>
                      {number(data.budget.charged_model_requests)} /{" "}
                      {data.budget.limits?.max_model_requests || "не задано"}
                    </dd>
                  </div>
                  <div>
                    <dt>Выходных токенов известно</dt>
                    <dd>{number(data.budget.known_output_tokens)}</dd>
                  </div>
                  <div>
                    <dt>Резерв текущих попыток</dt>
                    <dd>{number(data.budget.reserved_tokens)}</dd>
                  </div>
                  <div>
                    <dt>Неизвестный расход, случаев</dt>
                    <dd>{number(data.budget.unknown_usage)}</dd>
                  </div>
                  <div>
                    <dt>Токены с учетом неизвестного расхода</dt>
                    <dd>{number(data.budget.charged_tokens)}</dd>
                  </div>
                </dl>
                {data.execution.last_stop && (
                  <details>
                    <summary>Последняя остановка службы</summary>
                    <p>{data.execution.last_stop.at}</p>
                    <code>
                      {data.execution.last_stop.stage}:{" "}
                      {data.execution.last_stop.class}
                    </code>
                  </details>
                )}
                <div className="table-scroll">
                  <table>
                    <thead>
                      <tr>
                        <th>Попытка</th>
                        <th>Задача</th>
                        <th>Состояние</th>
                        <th>Диагностика</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.attempts.map((a) => (
                        <tr key={a.id}>
                          <td>
                            <code>{a.id}</code>
                          </td>
                          <td>{text(a.role)}</td>
                          <td>
                            {text(a.status)}
                            <small>
                              {a.remote_outcome === "unknown" &&
                              ["running", "preparing"].includes(a.status)
                                ? "Ожидается ответ"
                                : text(a.remote_outcome)}
                            </small>
                          </td>
                          <td>
                            {text(a.failure_class)}
                            <details>
                              <summary>Сетевые этапы</summary>
                              <pre>
                                {JSON.stringify(a.diagnostics || [], null, 2)}
                              </pre>
                            </details>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </>
            )}
          </div>
        </>
      )}
    </section>
  );
}
