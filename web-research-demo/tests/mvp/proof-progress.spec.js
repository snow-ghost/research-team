import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("учебное исследование показывает зависимости без запуска модели", async ({
  page,
}) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const headers = { Authorization: "Bearer " + ACCESS_KEY };
  const before = (
    await (await page.request.get("/api/bootstrap", { headers })).json()
  ).state;
  const created = await page.request.post("/api/research-pilots", {
    headers,
    data: {
      expected_revision: before.revision,
      request_id: "browser-pilot-creation",
      recipe: "injective-iterations@1",
      confirm: true,
    },
  });
  expect(created.ok()).toBeTruthy();
  const state = (await created.json()).state;
  expect(state.attempts.length).toBe(before.attempts.length);
  const study = state.studies.at(-1);
  const progress = await page.request.get(
    `/api/studies/${study.id}/proof-progress`,
    { headers },
  );
  expect(progress.ok()).toBeTruthy();
  expect((await progress.json()).nodes).toHaveLength(5);
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.getByRole("button", { name: study.title, exact: true }).click();
  await page.getByRole("tab", { name: /Задания/ }).click();
  const panel = page.getByRole("region", {
    name: "Обязательства доказательства",
  });
  await expect(panel.locator("tbody tr")).toHaveCount(5);
  await expect(panel).toContainText("Не принято основание");
  await page
    .getByRole("button", { name: "Создать команду", exact: true })
    .click();
  await page.getByLabel("Распределение задач").selectOption("demand");
  expect(await page.getByLabel("Распределение задач").inputValue()).toBe(
    "demand",
  );
  const close = page.getByRole("button", {
    name: "Закрыть сведения",
    exact: true,
  });
  if (await close.isVisible()) await close.click();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await panel.scrollIntoViewIfNeeded();
    await expect(panel).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: `test-results/proof-progress-${width}.png`,
      fullPage: true,
    });
  }
  expect(errors).toEqual([]);
});
