import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("цикл запускает доказательство, ждет приемки и завершает принятую цель", async ({
  page,
}) => {
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
    .fill("Исследовательский цикл");
  await dialog
    .getByLabel("Точное утверждение")
    .fill("Утверждение для проверки последовательности действий");
  await dialog
    .getByLabel("Область и предпосылки")
    .fill("Учебный конечный случай");
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  await expect(dialog).not.toBeVisible();
  await page.getByRole("tab", { name: /Задания/ }).click();
  await page.getByRole("button", { name: "Новый цикл", exact: true }).click();
  const form = page.getByRole("form", { name: "Новый исследовательский цикл" });
  await form.getByLabel("Предел попыток").fill("2");
  await page
    .getByRole("button", { name: "Закрыть сведения", exact: true })
    .click();
  await page.setViewportSize({ width: 320, height: 740 });
  await expect(form).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: "test-results/cycle-form-mobile.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 1440, height: 960 });
  await form.getByRole("button", { name: "Начать цикл" }).click();
  await expect(page.locator(".cycle-row")).toHaveCount(0);
  await form.getByRole("checkbox").check();
  await form.getByRole("button", { name: "Начать цикл" }).click();
  const row = page.locator(".cycle-row").first();
  await expect(row).toContainText("Ожидает приемки", { timeout: 15000 });
  await expect(row).toContainText("1 / 2");
  await page.screenshot({
    path: "test-results/cycle-desktop.png",
    fullPage: true,
  });
  await row
    .getByRole("button", { name: "Приостановить цикл", exact: true })
    .click();
  await expect(row).toContainText("Приостановлен");
  await row
    .getByRole("button", { name: "Продолжить цикл", exact: true })
    .click();
  await row.getByRole("checkbox").check();
  await row.getByRole("button", { name: "Продолжить", exact: true }).click();
  await expect(row).toContainText("Ожидает приемки");
  await row
    .getByRole("button", { name: "Открыть утверждение", exact: true })
    .click();
  await page.getByRole("button", { name: "Основания", exact: true }).click();
  await page
    .getByRole("button", { name: "Принять кандидат", exact: true })
    .click();
  await dialog
    .getByLabel("Обоснование решения")
    .fill("Проверено оператором в испытании последовательности действий.");
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  await expect(row).toContainText("Цель принята", { timeout: 10000 });
  await page.setViewportSize({ width: 390, height: 844 });
  await page
    .getByRole("button", { name: "Закрыть сведения", exact: true })
    .click();
  await expect(row).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: "test-results/cycle-mobile.png",
    fullPage: true,
  });
});
