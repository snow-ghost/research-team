import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("связи памяти создают отдельное обязательство, правила остаются в режиме наблюдения", async ({
  page,
}) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const headers = { Authorization: "Bearer " + ACCESS_KEY };
  const bootstrap = async () =>
    await (await page.request.get("/api/bootstrap", { headers })).json();
  const command = async (route, body) => {
    const { state } = await bootstrap();
    const response = await page.request.post("/api" + route, {
      headers,
      data: {
        ...body,
        expected_revision: state.revision,
        request_id: crypto.randomUUID(),
      },
    });
    expect(response.ok(), await response.text()).toBeTruthy();
    return (await response.json()).state;
  };
  const initial = await bootstrap(),
    wasPaused = initial.state.paused;
  if (!wasPaused) await command("/actions", { type: "PAUSE" });
  let state = await command("/actions", {
    type: "CREATE_STUDY",
    title: "Bool связи и выбор приема",
    statement: "Bool functions and injectivity",
    assumptions: "Bool",
  });
  const study = state.studies.at(-1);
  await command(`/entities/${study.goal}/formal-goal`, {
    goal: {
      source: "def BoolTarget : Prop := True",
      declaration: "BoolTarget",
      candidate: "boolTargetProof",
    },
  });
  state = await command("/actions", {
    type: "LEMMA",
    study: study.id,
    title: "Bool исходная запись",
    statement: "Bool functions source",
    assumptions: "Bool",
  });
  const source = state.entities.at(-1).id;
  await command(`/entities/${source}/formal-goal`, {
    goal: {
      source: "def BoolSource : Prop := True",
      declaration: "BoolSource",
      candidate: "boolSourceProof",
    },
  });
  await command("/memory", {
    target: source,
    kind: "note",
    content: "Bool functions требуют отдельной проверки условий",
    conditions: "Bool",
  });
  let branch;
  if (initial.lean.configured) {
    state = await command("/studies/" + study.id + "/budget", {
      budget: { max_attempts: 10, max_model_requests: 20 },
      note: "Наблюдение без модельных запусков",
      confirm: true,
    });
    state = await command("/branches", {
      method: "induction",
      rationale: "Проверить ограниченные правила",
      priority: 10,
      confirm: true,
      team: {
        study: study.id,
        target: study.goal,
        profiles: {
          proof: "fixture-proof",
          counterexample: "fixture-counterexample",
          formalize: "fixture-proof",
          review: "fixture-review",
        },
        workspace: "example",
        max_attempts: 6,
        require_lean: true,
      },
    });
    branch = state.branches.at(-1);
  }
  try {
    await page.goto("/");
    await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await page.getByRole("button", { name: study.title, exact: true }).click();
    await page.getByRole("tab", { name: /Задания/ }).click();
    const close = page.getByRole("button", {
      name: "Закрыть сведения",
      exact: true,
    });
    if (await close.isVisible()) await close.click();
    const memory = page.getByLabel("Связи памяти", { exact: true });
    await expect(memory).toContainText("Bool functions требуют");
    await memory
      .getByRole("button", { name: "Проверить связь условий", exact: true })
      .click();
    const form = page.getByRole("form", { name: "Обязательство связи памяти" });
    await form
      .getByLabel("Формальная связь", { exact: true })
      .fill("def BrowserConditions : Prop := True");
    await form
      .getByLabel("Декларация", { exact: true })
      .fill("BrowserConditions");
    await form
      .getByLabel("Кандидат", { exact: true })
      .fill("browserConditionProof");
    await form
      .getByLabel("Основание", { exact: true })
      .fill("Проверить объявленную связь отдельно от исхода прошлой попытки");
    await form.getByRole("checkbox").check();
    await form
      .getByRole("button", { name: "Создать обязательство связи", exact: true })
      .click();
    await expect(form).not.toBeVisible();
    await expect(memory).toContainText("открыто");
    if (branch) {
      const row = page
        .locator(".branch-list .team-row")
        .filter({ hasText: "Проверить ограниченные правила" });
      await row.getByText("Правила выбора ветви", { exact: true }).click();
      const policy = page.getByRole("form", {
        name: `Правила ветви ${branch.id}`,
      });
      await policy
        .getByRole("checkbox", { name: "Включить правила", exact: true })
        .check();
      await policy
        .getByRole("combobox", { name: "Режим", exact: true })
        .selectOption("observe");
      await policy.getByLabel("Предел решений", { exact: true }).fill("1");
      await policy.getByLabel("Неуспешных попыток", { exact: true }).fill("1");
      await policy.getByRole("checkbox", { name: /Подтверждаю/ }).check();
      await policy
        .getByRole("button", { name: "Сохранить правила", exact: true })
        .click();
      await expect(row).toContainText("Решений: 0 / 1");
    }
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 960 });
      await memory.scrollIntoViewIfNeeded();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      ).toBeTruthy();
      await page.screenshot({
        path: `test-results/memory-relations-${width}.png`,
        fullPage: true,
      });
    }
    await page.setViewportSize({ width: 1440, height: 960 });
    await page
      .getByRole("button", { name: "Библиотека лемм", exact: true })
      .click();
    await page.getByText("Замена представления", { exact: true }).click();
    const trial = page.getByRole("form", { name: "Опыт смены представления" });
    await expect(
      trial.getByRole("button", {
        name: "Проверить замену представления",
        exact: true,
      }),
    ).toBeDisabled();
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 960 });
      await trial.scrollIntoViewIfNeeded();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      ).toBeTruthy();
      await page.screenshot({
        path: `test-results/representation-form-${width}.png`,
        fullPage: true,
      });
    }
    expect(errors).toEqual([]);
  } finally {
    if (branch)
      await command(`/branches/${branch.id}/decisions`, {
        kind: "stop",
        note: "Завершение проверки интерфейса",
        confirm: true,
      });
    if (!wasPaused) await command("/actions", { type: "PAUSE" });
  }
});

test("наблюдение различает остановку процесса и последний этап, разбор остается предварительным", async ({
  page,
}) => {
  await page.route("**/api/evaluations", (route) =>
    route.fulfill({
      json: [
        {
          id: "observer-test",
          label: "Испытание наблюдения",
          status: "running",
          health: {
            process_state: "stopped",
            needs_attention: true,
            phase: "last_checkpoint_only",
            report_stale: false,
          },
        },
      ],
    }),
  );
  await page.route("**/api/evaluations/observer-test/analysis", (route) =>
    route.fulfill({
      json: {
        preliminary: true,
        independent_tasks: 1,
        repeat_observations: 1,
        classes: {
          functions: {
            paired: {
              single: {
                runs: 1,
                native_verified: 1,
                reviewed: 0,
                charged_requests: 2,
                seconds: 20,
              },
            },
          },
        },
        task_effects: [],
        review_gaps: [
          {
            case: "task-001",
            mode: "single",
            repetition: 1,
            review_status: "candidate",
            artifact_status: "verified_binding",
            findings: [
              {
                severity: "question",
                category: "premise",
                text: "Нужно уточнить условие",
                evidence: "Посылка отсутствует",
                location: "Шаг 2",
              },
            ],
          },
        ],
      },
    }),
  );
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.getByRole("tab", { name: /Задания/ }).click();
  const close = page.getByRole("button", {
    name: "Закрыть сведения",
    exact: true,
  });
  if (await close.isVisible()) await close.click();
  const watch = page.getByRole("region", { name: "Выполняющиеся сравнения" });
  await expect(watch).toContainText("Процесс: остановлен");
  await watch
    .getByRole("button", { name: "Разобрать результаты", exact: true })
    .click();
  const analysis = page.getByRole("region", { name: "Разбор сравнения" });
  await expect(analysis).toContainText("Предварительные данные");
  await analysis
    .getByText("Lean без завершенной рецензии: 1", { exact: true })
    .click();
  await expect(analysis).toContainText("Нужно уточнить условие");
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await analysis.scrollIntoViewIfNeeded();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: `test-results/comparison-analysis-${width}.png`,
      fullPage: true,
    });
  }
});
