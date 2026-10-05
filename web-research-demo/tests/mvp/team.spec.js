import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("команда выполняет отдельные задания, журнал виден на разных экранах", async ({
  page,
}) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page
    .getByRole("button", { name: "Новое исследование", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Название", { exact: true })
    .fill("Команда для учебной гипотезы");
  await dialog
    .getByLabel("Точное утверждение")
    .fill("Утверждение с независимыми заданиями.");
  await dialog
    .getByLabel("Область и предпосылки")
    .fill("Конечный учебный случай");
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  await page.getByRole("tab", { name: /Задания/ }).click();
  await page
    .getByRole("button", { name: "Настроить бюджет", exact: true })
    .click();
  const budgetForm = page.getByRole("form", {
    name: "Настройка общего бюджета",
  });
  await budgetForm
    .getByLabel("Попыток на исследование", { exact: true })
    .fill("20");
  await budgetForm
    .getByLabel("Выходных токенов на исследование", { exact: true })
    .fill("0");
  await budgetForm
    .getByLabel("Основание изменения")
    .fill("Подтвержденный бюджет браузерного испытания");
  await budgetForm.getByRole("checkbox", { name: /Подтверждаю/ }).check();
  await budgetForm
    .getByRole("button", { name: "Сохранить бюджет", exact: true })
    .click();
  await expect(budgetForm).not.toBeVisible();
  await expect(
    page.getByLabel("Общий бюджет исследования", { exact: true }),
  ).toContainText("/20");
  await page
    .getByRole("button", { name: "Создать команду", exact: true })
    .click();
  const form = page.getByRole("form", { name: "Настройка команды" });
  await form.getByLabel("Обязательство", { exact: true }).selectOption({
    label: "Команда для учебной гипотезы",
  });
  await form
    .getByLabel("Поиск доказательства", { exact: true })
    .selectOption("fixture-proof");
  await form
    .getByLabel("Поиск контрпримеров", { exact: true })
    .selectOption("fixture-counterexample");
  await form
    .getByLabel("Рецензия", { exact: true })
    .selectOption("fixture-review");
  await form
    .getByLabel("Формализация", { exact: true })
    .selectOption("fixture-proof");
  const lean = form.getByRole("checkbox", {
    name: "Проверка Lean",
    exact: true,
  });
  if (await lean.isEnabled()) await lean.uncheck();
  await form.getByRole("checkbox", { name: /Подтверждаю/ }).check();
  await form.getByRole("button", { name: "Начать", exact: true }).click();
  const team = page.locator(".team-row").first();
  await expect(team).toContainText("Ожидает приемки", { timeout: 15000 });
  await expect(team).toContainText("3/8");
  await page
    .locator(".attempt-row")
    .first()
    .getByRole("button", { name: /Журнал действий/ })
    .click();
  const journal = page.getByRole("region", { name: "Журнал попытки" });
  await expect(journal).toContainText("Ответ модели");
  await expect(journal).toContainText("Токены");
  await expect(journal).toContainText("Вход:");
  await expect(journal).toContainText("Выход:");
  await team.getByTitle("Приостановить новые задания").click();
  await expect(team).toContainText("Приостановлена");
  await team
    .getByRole("button", { name: "Продолжить команду", exact: true })
    .click();
  const limitsForm = page.getByRole("form", { name: "Продолжение команды" });
  await limitsForm
    .getByLabel("Общий предел попыток", { exact: true })
    .fill("9");
  await limitsForm
    .getByLabel("Поиск доказательства: токенов на ответ", { exact: true })
    .fill("16384");
  await limitsForm.getByRole("checkbox", { name: /Подтверждаю/ }).check();
  await limitsForm
    .getByRole("button", { name: "Сохранить ограничения", exact: true })
    .click();
  await expect(limitsForm).not.toBeVisible();
  await expect(team).toContainText("Приостановлена");
  await expect(team).toContainText("3/9");
  await expect(page.locator(".attempt-row")).toHaveCount(3);
  await team
    .getByRole("button", { name: "Продолжить команду", exact: true })
    .click();
  await expect(
    limitsForm.getByLabel("Общий предел попыток", { exact: true }),
  ).toHaveValue("9");
  await expect(
    limitsForm.getByLabel("Поиск доказательства: токенов на ответ", {
      exact: true,
    }),
  ).toHaveValue("16384");
  await limitsForm
    .getByRole("button", { name: "Закрыть", exact: true })
    .click();
  await page.locator(".runtime-panel").evaluate((element) => {
    element.scrollTop = 0;
  });
  await page.screenshot({
    path: "test-results/team-desktop.png",
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "Закрыть сведения", exact: true })
    .click();
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    await page.locator(".runtime-panel").evaluate((element) => {
      element.scrollTop = 0;
    });
    await expect(team).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    const bad = await page
      .locator(".role-strip > div")
      .evaluateAll((nodes) =>
        nodes.some((n) => n.scrollWidth > n.clientWidth + 1),
      );
    expect(bad).toBeFalsy();
    await page.screenshot({
      path: "test-results/team-mobile-" + width + ".png",
      fullPage: true,
    });
  }
  expect(errors).toEqual([]);
  await page
    .getByRole("button", { name: "Просмотреть отчет", exact: true })
    .click();
  await expect(
    page.getByRole("region", { name: "Отчет исследования" }),
  ).toContainText("Команда для учебной гипотезы");
});
