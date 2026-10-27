import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("полный цикл требует подтверждения, сохраняет паузу и не запускает очередь сам", async ({
  page,
}) => {
  const headers = { Authorization: "Bearer " + ACCESS_KEY },
    errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const read = async () =>
    await (await page.request.get("/api/bootstrap", { headers })).json();
  const command = async (route, body) => {
    const { state } = await read();
    const r = await page.request.post("/api" + route, {
      headers,
      data: {
        ...body,
        expected_revision: state.revision,
        request_id: crypto.randomUUID(),
      },
    });
    expect(r.ok(), await r.text()).toBeTruthy();
    return (await r.json()).state;
  };
  const initial = await read();
  if (!initial.state.paused) await command("/actions", { type: "PAUSE" });
  const state = await command("/actions", {
      type: "CREATE_STUDY",
      title: "Управление полным циклом",
      statement: "True",
      assumptions: "Prop",
    }),
    study = state.studies.at(-1);
  await command(`/entities/${study.goal}/formal-goal`, {
    goal: {
      source: "def WorkflowBrowser : Prop := True",
      declaration: "WorkflowBrowser",
      candidate: "workflowBrowserProof",
    },
  });
  await command(`/studies/${study.id}/budget`, {
    budget: { max_attempts: 12, max_model_requests: 24 },
    note: "Только подготовка; разрешения на вызов модели нет",
    confirm: true,
  });
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
    const panel = page.getByLabel("Полный исследовательский цикл", {
      exact: true,
    });
    await panel
      .getByRole("button", { name: "Подготовить", exact: true })
      .click();
    const form = page.getByRole("form", { name: "Подготовка полного цикла" });
    await form
      .getByRole("combobox", { name: "Планировщик", exact: true })
      .selectOption("fixture-reader");
    for (const [label, value] of [
      ["Доказательство", "fixture-proof"],
      ["Контрпримеры", "fixture-counterexample"],
      ["Формализация", "fixture-proof"],
      ["Рецензия", "fixture-review"],
    ])
      await form
        .getByRole("combobox", { name: label, exact: true })
        .selectOption(value);
    await form.getByRole("checkbox").check();
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 960 });
      await form.scrollIntoViewIfNeeded();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBeTruthy();
      await page.screenshot({
        path: `test-results/workflow-form-${width}.png`,
        fullPage: false,
      });
    }
    await form
      .getByRole("button", { name: "Подготовить цикл", exact: true })
      .click();
    if (initial.lean.configured) {
      await expect(form).not.toBeVisible();
      await expect(panel).toContainText("Подготовлен");
      await panel.getByRole("button", { name: "Пауза", exact: true }).click();
      const decision = panel.getByRole("form", {
        name: "Подтверждаю паузу цикла",
      });
      await decision
        .getByLabel("Основание", { exact: true })
        .fill("Сохранить подготовленный цикл без вызова модели");
      await decision.getByRole("checkbox").check();
      await decision
        .getByRole("button", { name: "Подтвердить", exact: true })
        .click();
      await expect(panel).toContainText("Приостановлен");
      await panel
        .getByRole("button", { name: "Остановить", exact: true })
        .click();
      const stop = panel.getByRole("form", {
        name: "Подтверждаю остановку цикла",
      });
      await stop
        .getByLabel("Основание", { exact: true })
        .fill("Испытание управления завершено");
      await stop.getByRole("checkbox").check();
      await stop
        .getByRole("button", { name: "Подтвердить", exact: true })
        .click();
      await expect(panel).toContainText("Остановлен");
    } else {
      await expect(panel.getByRole("alert")).toContainText("Lean");
    }
    const after = await read();
    expect(after.state.paused).toBeTruthy();
    expect(after.state.attempts.length).toBe(initial.state.attempts.length);
    expect(errors).toEqual([]);
  } finally {
    if (!initial.state.paused)
      await command("/actions", { type: "PAUSE" }).catch(() => {});
  }
});
