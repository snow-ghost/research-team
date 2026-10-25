import React, { useState } from "react";
import { GitBranch, ChevronRight, FileSearch } from "lucide-react";

const origins = {
  planner: "Планировщик",
  operator: "Оператор",
  legacy_unclassified: "Источник не классифицирован",
};
const transmissions = {
  not_started: "Обращение не начато",
  unknown: "Исход обращения неизвестен",
  response_received: "Ответ получен",
};
const noteKinds = {
  operational: "Организационная заметка",
  mathematical_hint: "Математическая подсказка",
  unspecified: "Вид не указан",
};
const statuses = {
  open: "Открыто",
  accepted: "Принято",
  refuted: "Опровергнуто",
  in_review: "На рецензии",
  needs_changes: "Нужны исправления",
  blocked: "Ожидание",
  stale: "Устарело",
  ready: "Готово",
  queued: "В очереди",
  running: "В работе",
  completed: "Завершено",
  failed: "Ошибка",
  cancelled: "Отменено",
  candidate: "Кандидат",
  verified: "Проверено",
  interrupted: "Прервано",
  abandoned: "Ветвь закрыта",
  paused: "Приостановлено",
};
const roles = {
  proof: "Доказательство",
  counterexample: "Контрпримеры",
  formalize: "Формализация",
  review: "Рецензия",
  decompose: "Планирование",
};

function DependencyTree({ progress, onSelect }) {
  const nodes = new Map(progress.nodes.map((n) => [n.id, n]));
  let shown = 0;
  const walk = (id, path = []) => {
    const n = nodes.get(id);
    if (++shown > 128) return <li key={id}>Предел отображения дерева</li>;
    if (!n) return <li key={id}>{id}: вне исследования</li>;
    if (path.includes(id) || path.length >= 16)
      return <li key={id}>{n.title}: повторная ссылка</li>;
    return (
      <li key={id}>
        <button className="audit-tree-node" onClick={() => onSelect?.(id)}>
          <ChevronRight size={14} />
          <span>{n.title}</span>
          <small>{statuses[n.status] || n.status}</small>
        </button>
        {!!n.dependencies?.length && (
          <ul>{n.dependencies.map((dep) => walk(dep, [...path, id]))}</ul>
        )}
      </li>
    );
  };
  return (
    <div className="audit-tree" aria-label="Дерево оснований">
      <ul>{walk(progress.goal)}</ul>
    </div>
  );
}

function CheckDetails({ check, onSelect }) {
  return (
    <details className="proof-audit-check">
      <summary>
        <FileSearch size={15} />
        {check.id} · {statuses[check.status] || check.status}
        {check.current ? " · действующая проверка" : ""}
      </summary>
      <dl className="audit-bindings">
        <dt>Исходник</dt>
        <dd>{check.report?.source_sha256 || "Нет контрольной суммы"}</dd>
        <dt>Цель</dt>
        <dd>{check.report?.goal_sha256 || "Нет контрольной суммы"}</dd>
        <dt>Попытка</dt>
        <dd>{check.attempt || "Представленный исходник"}</dd>
        <dt>Зависимости терма</dt>
        <dd>
          {check.dependency_audit_current
            ? "Проверены"
            : "Нет действующего отчета"}
        </dd>
      </dl>
      {!!check.missing_required_libraries?.length && (
        <p role="status">
          Не выполнено обязательное использование лемм:{" "}
          {check.missing_required_libraries.join(", ")}
        </p>
      )}
      {!!check.lemmas?.length && (
        <ul className="audit-lemma-list">
          {check.lemmas.map((l) => (
            <li key={l.library}>
              <button
                className="audit-tree-node"
                onClick={() => onSelect?.(l.target)}
              >
                <ChevronRight size={14} />
                <span>{l.title}</span>
              </button>
              <p>
                {l.required ? "Обязательная лемма" : "Модуль доступен проверке"}{" "}
                ·{" "}
                {check.dependency_audit_current
                  ? l.referenced
                    ? "Есть ссылка в терме"
                    : "Ссылки в терме нет"
                  : "Использование не установлено"}
              </p>
              <code>{l.declaration}</code>
            </li>
          ))}
        </ul>
      )}
      <details>
        <summary>Точный исходник Lean</summary>
        <pre className="audit-source">{check.source}</pre>
      </details>
      <details>
        <summary>Закрепленная цель Lean</summary>
        <pre className="audit-source">{check.goal.source}</pre>
      </details>
      <details>
        <summary>Отчет проверки</summary>
        <pre className="audit-source">
          {JSON.stringify(check.report, null, 2)}
        </pre>
      </details>
    </details>
  );
}

export function ResearchAudit({ progress, state, onSelect }) {
  const [selected, setSelected] = useState("");
  const node =
    progress.nodes.find((n) => n.id === selected) ||
    progress.nodes.find((n) => n.id === progress.goal);
  const select = (id) => {
    setSelected(id);
  };
  const findings = (state.findings || []).filter((f) => f.target === node?.id);
  const teams = (state.teams || []).filter(
    (t) => (t.target || t.goal) === node?.id,
  );
  return (
    <section className="research-audit" aria-label="Аудит исследования">
      <h3>
        <GitBranch size={17} />
        Аудит исследования
      </h3>
      <DependencyTree progress={progress} onSelect={select} />
      <label className="audit-select">
        Утверждение для аудита
        <select
          value={node?.id || ""}
          onChange={(e) => setSelected(e.target.value)}
        >
          {progress.nodes.map((n) => (
            <option key={n.id} value={n.id}>
              {n.title}
            </option>
          ))}
        </select>
      </label>
      {node && (
        <div className="audit-content">
          <h4>{node.title}</h4>
          {onSelect && (
            <button
              className="button secondary"
              onClick={() => onSelect(node.id)}
            >
              <ChevronRight size={15} />
              Открыть утверждение
            </button>
          )}
          <h4>Решения по ветвям</h4>
          {!node.branches?.length && <p>Ветви не назначены</p>}
          {node.branches?.map((b) => (
            <details key={b.id}>
              <summary>
                {b.id} · {statuses[b.status] || b.status}
              </summary>
              <p>{b.reason}</p>
              <p>Источник: {origins[b.basis?.source] || "Не установлен"}</p>
              {b.basis && (
                <>
                  <p>{b.basis.rationale}</p>
                  <p>
                    План: {b.basis.plan || "Не назначен"}. Попытка:{" "}
                    {b.basis.attempt || "Не назначена"}.
                  </p>
                  <code>{b.basis.result_sha256}</code>
                </>
              )}
            </details>
          ))}
          {teams.map((t) => (
            <div className="audit-notes" key={t.id}>
              {t.method_basis && (
                <p>
                  Основание метода:{" "}
                  {origins[t.method_basis.source] || "Не установлен"}.{" "}
                  {t.method_basis.rationale}
                </p>
              )}
              {!t.operator_notes?.length && t.operator_note && (
                <p>
                  Историческая заметка, источник не классифицирован:{" "}
                  {t.operator_note}
                </p>
              )}
              {t.operator_notes?.map((note, i) => (
                <p key={i}>
                  {noteKinds[note.kind] || "Вид не указан"}: {note.text}
                </p>
              ))}
            </div>
          ))}
          <h4>Исходники и проверки</h4>
          {!node.checks?.length && <p>Проверки не назначены</p>}
          {node.checks?.map((c) => (
            <CheckDetails key={c.id} check={c} onSelect={select} />
          ))}
          <h4>Замечания рецензента</h4>
          {!findings.length && <p>Замечания не зарегистрированы</p>}
          <ul>
            {findings.map((f) => (
              <li key={f.id}>
                {f.text} · {f.state}
                <p>{f.location}</p>
                <code>{f.source_text_sha256}</code>
              </li>
            ))}
          </ul>
          <h4>Попытки и остановки</h4>
          {!node.attempts?.length && <p>Попытки не назначены</p>}
          <ul>
            {node.attempts?.map((a) => (
              <li key={a.id}>
                <code>{a.id}</code>
                <p>
                  {roles[a.role] || a.role} · {statuses[a.status] || a.status} ·{" "}
                  {transmissions[a.remote_outcome] ||
                    a.remote_outcome ||
                    "Нет сведений об обращении"}
                </p>
                {a.stop_cause && <p>Причина остановки: {a.stop_cause}</p>}
                {!!a.missing_materials?.length && (
                  <p>
                    Отсутствующие материалы: {a.missing_materials.join(", ")}
                  </p>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
