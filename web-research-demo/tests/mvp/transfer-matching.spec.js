import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("подбор показывает открытую цель, поврежденный перенос не изменяет базу", async ({
  page,
}) => {
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const headers = { Authorization: "Bearer " + ACCESS_KEY };
  const snapshot = async () =>
    (await (await page.request.get("/api/bootstrap", { headers })).json())
      .state;
  const command = async (route, body) => {
    const state = await snapshot();
    const response = await page.request.post("/api" + route, {
      headers,
      data: {
        ...body,
        expected_revision: state.revision,
        request_id: "browser-match-" + Date.now(),
      },
    });
    expect(response.ok(), await response.text()).toBeTruthy();
    return (await response.json()).state;
  };
  const wasPaused = (await snapshot()).paused;
  let state = await command("/actions", {
    type: "CREATE_STUDY",
    title: "Подбор леммы без модели",
    statement: "True",
    assumptions: "Prop",
  });
  const study = state.studies.at(-1);
  await command("/entities/" + study.goal + "/formal-goal", {
    goal: {
      source: "def BrowserMatchGoal : Prop := True\n",
      declaration: "BrowserMatchGoal",
      candidate: "BrowserMatchCandidate",
    },
  });
  if (!wasPaused) await command("/actions", { type: "PAUSE" });
  try {
    await page.goto("/");
    await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await page.getByRole("button", { name: study.title, exact: true }).click();
    await page
      .getByRole("button", { name: "Библиотека лемм", exact: true })
      .click();
    const matcher = page.getByRole("region", {
      name: "Подбор типов Lean",
      exact: true,
    });
    await expect(
      matcher.getByRole("combobox", { name: "Цель", exact: true }),
    ).toHaveValue(study.goal);
    await expect(
      matcher.getByRole("button", {
        name: "Подобрать подстановку",
        exact: true,
      }),
    ).toBeDisabled();
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 960 });
      await matcher.scrollIntoViewIfNeeded();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      ).toBeTruthy();
      await page.screenshot({
        path: `test-results/lemma-match-${width}.png`,
        fullPage: true,
      });
    }
    await page.setViewportSize({ width: 1440, height: 960 });
    await page
      .getByRole("button", { name: "Исследования", exact: true })
      .click();
    await page.getByRole("tab", { name: /Задания/ }).click();
    const evidence = page.getByRole("region", {
      name: "Проверяемые результаты",
    });
    await evidence
      .getByRole("button", { name: "Перенести пакет результата", exact: true })
      .click();
    const form = evidence.getByRole("form", { name: "Перенос результата" });
    await form.getByLabel("Пакет результата", { exact: true }).setInputFiles({
      name: "invalid-package.json",
      mimeType: "application/json",
      buffer: Buffer.from('{"version":1}'),
    });
    await form
      .getByRole("checkbox", {
        name: "Подтверждаю перенос и новую проверку Lean",
      })
      .check();
    const before = await snapshot();
    await form.getByRole("button", { name: "Перенести", exact: true }).click();
    await expect(evidence.getByRole("alert")).toBeVisible();
    state = await snapshot();
    expect(state.revision).toBe(before.revision);
    expect(state.attempts).toHaveLength(before.attempts.length);
    expect(state.results || []).toHaveLength((before.results || []).length);
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 960 });
      await form.scrollIntoViewIfNeeded();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      ).toBeTruthy();
      await page.screenshot({
        path: `test-results/result-transfer-${width}.png`,
        fullPage: true,
      });
    }
    expect(errors).toEqual([]);
  } finally {
    if (!wasPaused && (await snapshot()).paused)
      await command("/actions", { type: "PAUSE" });
  }
});

test("сравнение разделяет результат Lean и отказ рецензии по этапам", async ({
  page,
}) => {
  await page.route("**/api/bootstrap", async (route) => {
    const response = await route.fetch();
    const view = await response.json();
    view.state.comparisons = [
      {
        id: "browser-demand-comparison",
        created_at: "2026-10-13T18:00:00Z",
        report: {
          version: 3,
          runs: [
            {
              case: "educational-case",
              mode: "demand",
              repetition: 1,
              status: "failed",
              mathematical_status: "proof_verified",
              measured_requests: 2,
              request_upper_bound: 2,
              input_tokens: 20,
              output_tokens: 64,
              seconds: 2.5,
              usage_incomplete: true,
              verification: {
                status: "verified",
                audit_sha256: "a".repeat(64),
              },
              review_accepted: false,
              error: "executor failed; see private result diagnostics",
              stages: [
                {
                  role: "proof",
                  status: "candidate",
                  requests: 1,
                  seconds: 1,
                  usage: { output_tokens: 64 },
                  profile_sha256: "b".repeat(64),
                  result_sha256: "c".repeat(64),
                },
                {
                  role: "review",
                  status: "failed",
                  failure_code: "execution_failed",
                  provider_http_status: 429,
                  requests: 1,
                  seconds: 1.5,
                  profile_sha256: "d".repeat(64),
                  result_sha256: "e".repeat(64),
                },
              ],
            },
          ],
        },
      },
    ];
    await route.fulfill({ response, json: view });
  });
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.getByRole("button", { name: "Исполнители", exact: true }).click();
  const panel = page.getByRole("region", { name: "Сравнение исполнителей" });
  await expect(panel).toContainText("Специалисты по необходимости");
  await expect(panel).toContainText("Доказательство проверено");
  await expect(panel).toContainText("Lean: проверено");
  await panel.getByText("Этапы", { exact: true }).click();
  await expect(panel).toContainText("HTTP: 429");
  await expect(panel).toContainText("Выходные токены: не полностью измерено");
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await panel.scrollIntoViewIfNeeded();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: `test-results/comparison-stages-${width}.png`,
      fullPage: true,
    });
  }
});
