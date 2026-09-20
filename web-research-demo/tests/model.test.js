import test from "node:test";
import assert from "node:assert/strict";
import {
  createInitialState,
  transition,
  effectiveStatus,
  isStoredState,
} from "../src/model.js";
const get = (s, id) => s.entities.find((x) => x.id === id);

test("Исходный снимок: общая цель открыта, леммы приняты", () => {
  const s = createInitialState();
  assert.equal(effectiveStatus(s, "H0"), "open");
  assert.equal(effectiveStatus(s, "L1"), "accepted");
  assert.equal(effectiveStatus(s, "H-bad"), "refuted");
  assert.ok(isStoredState(s));
});
test("Разбиение создает случаи и явное обязательство покрытия", () => {
  const initial = createInitialState();
  const s = transition(initial, {
    type: "SPLIT",
    target: "H2",
    parts: ["Один компонент", "Несколько компонент"],
  });
  assert.equal(s.entities.length, initial.entities.length + 3);
  assert.equal(get(s, "O-2").kind, "obligation");
  assert.equal(get(s, "H2").status, "open");
  assert.equal(get(initial, "H2").revision, 1);
  assert.equal(get(s, "H2").revision, 2);
});
test("Запрос поиска контрпримера не опровергает цель", () => {
  const s = transition(createInitialState(), {
    type: "COUNTEREXAMPLE",
    target: "H0",
  });
  assert.equal(get(s, "H0").status, "open");
  assert.equal(s.tasks.at(-1).state, "queued");
});
test("Лемма-кандидат не принимается автором", () => {
  let s = transition(createInitialState(), {
    type: "LEMMA",
    title: "Лемма",
    statement: "P",
    assumptions: "A",
  });
  assert.equal(get(s, "L-2").status, "open");
  s = transition(s, { type: "SUBMIT_REVIEW", target: "L-2" });
  assert.throws(
    () => transition(s, { type: "REVIEW", target: "L-2", decision: "accept" }),
    /отдельного проверяющего/,
  );
});
test("Применение создает условный переход, исходная цель не закрывается", () => {
  const s = transition(createInitialState(), {
    type: "APPLY",
    lemma: "L2",
    target: "H3",
  });
  assert.equal(s.applications.length, 1);
  assert.equal(get(s, "A-2").status, "in_review");
  assert.equal(get(s, "H3").status, "open");
  assert.ok(get(s, "H3").dependencies.includes("A-2"));
  assert.throws(
    () => transition(s, { type: "APPLY", lemma: "L2", target: "H3" }),
    /уже предложено/,
  );
});
test("Оспаривание распространяется через точные зависимости", () => {
  const s = transition(createInitialState(), {
    type: "CHALLENGE",
    target: "L1",
    text: "Уточнить область",
  });
  assert.equal(effectiveStatus(s, "H1"), "blocked");
  assert.equal(effectiveStatus(s, "H2"), "blocked");
  assert.equal(effectiveStatus(s, "H0"), "blocked");
  assert.notEqual(get(s, "H0").status, "refuted");
});
test("Оспоренная лемма не применяется в другом исследовании", () => {
  const s = transition(createInitialState(), {
    type: "CHALLENGE",
    target: "L2",
    text: "Проверить определения",
  });
  assert.throws(
    () => transition(s, { type: "APPLY", lemma: "L2", target: "H3" }),
    /действующую/,
  );
});
test("Отказ в приемке не равен опровержению утверждения", () => {
  const s = transition(createInitialState(), {
    type: "REVIEW",
    target: "H1",
    decision: "reject",
  });
  assert.equal(get(s, "H1").status, "needs_changes");
  assert.equal(get(s, "H0").status, "open");
});
test("Открытое математическое замечание блокирует приемку", () => {
  const s = transition(createInitialState(), {
    type: "FINDING",
    target: "H1",
    text: "Не проверена база",
    severity: "major",
  });
  assert.throws(
    () => transition(s, { type: "REVIEW", target: "H1", decision: "accept" }),
    /замечани/,
  );
});
test("Вопрос не меняет постановку и сохраняет версию снимка", () => {
  const s = transition(createInitialState(), {
    type: "QUESTION",
    target: "H0",
    text: "Что еще нужно проверить?",
  });
  assert.equal(s.questions[0].snapshot, 1);
  assert.match(s.questions[0].answer, /H0@1/);
  assert.equal(get(s, "H0").revision, 1);
});
test("Пауза останавливает демонстрационный сценарий", () => {
  const s = transition(createInitialState(), { type: "PAUSE" });
  assert.throws(() => transition(s, { type: "STEP" }), /приостановлен/);
});
test("Три события показывают повторное использование, но не закрывают H3", () => {
  let s = createInitialState();
  for (let i = 0; i < 3; i++) s = transition(s, { type: "STEP" });
  assert.equal(s.scenarioStep, 3);
  assert.equal(get(s, s.applications[0].id).status, "accepted");
  assert.equal(get(s, "H3").status, "open");
  assert.equal(
    s.history[0].snapshot.entities.find((x) => x.id === "H1").status,
    "in_review",
  );
  assert.throws(() => transition(s, { type: "STEP" }), /уже показаны/);
});
test("Ответ автора оставляет замечание на перепроверке", () => {
  let s = transition(createInitialState(), {
    type: "FINDING",
    target: "H1",
    text: "Проверить",
    severity: "question",
  });
  s = transition(s, { type: "RESOLVE_FINDING", finding: s.findings[0].id });
  assert.equal(s.findings[0].state, "verification_pending");
});
test("Поврежденный сохраненный снимок отвергается", () => {
  assert.equal(isStoredState(null), false);
  const s = createInitialState();
  s.history[0].snapshot.entities = null;
  assert.equal(isStoredState(s), false);
});

test("Новое применение не переписывает принятую версию", () => {
  const s = transition(createInitialState(), {
    type: "REVIEW",
    target: "H1",
    decision: "accept",
  });
  assert.throws(
    () => transition(s, { type: "APPLY", lemma: "L2", target: "H1" }),
    /версия не изменяется/,
  );
});
