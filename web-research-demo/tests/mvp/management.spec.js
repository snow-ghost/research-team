import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("историческое сравнение предупреждает о повторном аудите цели", async ({
  page,
}) => {
  const headers = { Authorization: "Bearer " + ACCESS_KEY };
  const state = (
    await (await page.request.get("/api/bootstrap", { headers })).json()
  ).state;
  const runs = Array.from({ length: 6 }, (_, index) =>
    ["single", "team"].map((mode) => ({
      case: `legacy-${index}`,
      mode,
      goal_sha256: "a".repeat(64),
      status: "verified_not_accepted",
      outcome: "proof",
      request_upper_bound: 1,
      measured_requests: 1,
      unknown_request_count: false,
      input_tokens: 10,
      output_tokens: 20,
      usage_incomplete: false,
      seconds: 1,
      profiles: {},
      verification: { status: "verified", phase: "complete", axioms: [] },
      review_accepted: true,
    })),
  ).flat();
  let response;
  for (let retry = 0; retry < 5; retry++) {
    const current = (
      await (await page.request.get("/api/bootstrap", { headers })).json()
    ).state;
    response = await page.request.post("/api/comparisons", {
      headers,
      data: {
        expected_revision: current.revision,
        request_id: `browser-legacy-comparison-${retry}`,
        confirm: true,
        report: {
          version: 1,
          started_at: "2026-10-07T00:00:00Z",
          configuration_sha256: "b".repeat(64),
          max_requests: 96,
          runs,
        },
      },
    });
    if (response.status() !== 409) break;
  }
  expect(response.ok()).toBeTruthy();
  const after = (await response.json()).state;
  expect(
    after.entities.filter((entity) => entity.status === "accepted").length,
  ).toBe(
    state.entities.filter((entity) => entity.status === "accepted").length,
  );
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.getByRole("button", { name: "Исполнители", exact: true }).click();
  const panel = page.getByRole("region", { name: "Сравнение исполнителей" });
  await expect(panel.getByRole("alert")).toContainText(
    "без контроля происхождения цели",
  );
  await expect(panel.locator("tbody tr")).toHaveCount(12);
  await expect(
    panel.getByRole("columnheader", { name: "Запросы, измерено", exact: true }),
  ).toBeVisible();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    const table = await panel.locator("table").boundingBox();
    const mode = await panel
      .locator("tbody tr")
      .first()
      .locator("td")
      .nth(1)
      .boundingBox();
    expect(table.width).toBeGreaterThanOrEqual(1100);
    expect(mode.width).toBeGreaterThanOrEqual(160);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
  }
});

test("версии профилей сохраняют навыки и доступны после обновления", async ({
  page,
}) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.getByRole("button", { name: "Исполнители", exact: true }).click();
  const factory = page.getByRole("region", { name: "Фабрика агентов" });
  await factory.getByRole("button", { name: "Создать версию" }).click();
  const form = factory.getByRole("form", { name: "Создание профиля" });
  await form
    .getByLabel("Имя", { exact: true })
    .first()
    .fill("browser-researcher");
  await form
    .getByLabel("Название", { exact: true })
    .fill("Исследователь с индукцией");
  await form.getByRole("button", { name: "Добавить навык" }).click();
  const skill = form.locator(".skill-editor").last();
  await skill.getByLabel("Имя", { exact: true }).fill("induction");
  await skill
    .getByLabel("Инструкции")
    .fill("Проверить базу индукции и полный шаг.");
  await form.getByLabel("Подтверждаю модель, навыки и ограничения").check();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await form.scrollIntoViewIfNeeded();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: `test-results/profiles-${width}.png`,
      fullPage: true,
    });
  }
  await form
    .getByRole("button", { name: "Создать версию", exact: true })
    .click();
  await expect(form).not.toBeVisible();
  await expect(factory).toContainText("browser-researcher@1");
  await page.reload();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.getByRole("button", { name: "Исполнители", exact: true }).click();
  await expect(factory).toContainText("browser-researcher@1");
  expect(errors).toEqual([]);
});

test("план создает лемму и покрытие только после подтверждения", async ({
  page,
}) => {
  test.skip(
    !process.env.MVP_LEAN_CONFIG,
    "Real Lean enables approved method branches",
  );
  const headers = { Authorization: "Bearer " + ACCESS_KEY };
  const snapshot = async () =>
    (await page.request.get("/api/bootstrap", { headers })).json();
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page
    .getByRole("button", { name: "Новое исследование", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Название", { exact: true })
    .fill("Проверка предложения плана");
  await dialog.getByLabel("Точное утверждение").fill("True");
  await dialog.getByLabel("Область и предпосылки").fill("Prop");
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  await page.getByRole("tab", { name: /Задания/ }).click();
  await page
    .getByRole("button", { name: "Закрепить цель Lean", exact: true })
    .click();
  await page.getByLabel("Имя цели", { exact: true }).fill("Statement");
  await page.getByLabel("Имя теоремы кандидата").fill("Candidate");
  await page
    .getByLabel("Исходный текст Goal.lean")
    .fill("def Statement : Prop := True\n");
  await page
    .getByRole("button", { name: "Закрепить версию", exact: true })
    .click();
  const panel = page.getByRole("region", { name: "Планы исследования" });
  await panel
    .getByRole("button", { name: "Запросить план", exact: true })
    .click();
  const form = panel.getByRole("form", { name: "Запрос плана" });
  await form.getByLabel("Планировщик").selectOption("fixture-proof");
  await form.getByLabel("Подтверждаю запрос плана и расходы").check();
  await form.getByRole("button", { name: "Запросить", exact: true }).click();
  await panel
    .getByRole("button", { name: "Записать предложение", exact: true })
    .click();
  await expect(panel).toContainText("Независимая лемма плана");
  let view = await snapshot();
  const proposal = view.state.proposals.find((p) =>
    p.report.summary.includes("Сначала лемма"),
  );
  expect(proposal.status).toBe("proposed");
  expect(
    (view.state.branches || []).some((b) => b.plan === proposal.id),
  ).toBeFalsy();
  await panel
    .getByRole("button", { name: "Проверить и подтвердить", exact: true })
    .click();
  const approval = panel.getByRole("form", { name: "Подтверждение плана" });
  await approval
    .getByRole("combobox", { name: "Формализация", exact: true })
    .selectOption("fixture-proof");
  await approval
    .getByLabel("Подтверждаю предпосылки, покрытие, цели и расходы ветвей")
    .check();
  await approval
    .getByRole("button", { name: "Подтвердить план", exact: true })
    .click();
  await expect(panel).toContainText("Подтвержден");
  view = await snapshot();
  const applied = view.state.proposals.find((p) => p.id === proposal.id);
  expect(applied.children).toHaveLength(2);
  expect(
    view.state.branches.filter((b) => b.plan === proposal.id),
  ).toHaveLength(3);
  expect(view.state.entities.find((e) => e.id === applied.target).status).toBe(
    "open",
  );
  for (const branch of view.state.branches.filter(
    (b) => b.plan === proposal.id,
  )) {
    for (let retry = 0; retry < 5; retry++) {
      const fresh = await snapshot();
      const response = await page.request.post(
        `/api/branches/${branch.id}/commands`,
        {
          headers,
          data: {
            kind: "abandon",
            note: "Завершение отдельного браузерного испытания.",
            expected_revision: fresh.state.revision,
            request_id: `browser-stop-${Date.now()}-${retry}`,
          },
        },
      );
      if (response.status() === 409) continue;
      expect(response.ok()).toBeTruthy();
      break;
    }
  }
});
