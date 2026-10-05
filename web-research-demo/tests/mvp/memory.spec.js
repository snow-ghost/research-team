import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("память сохраняет заметку, показывает доверие и не выходит за пределы экрана", async ({
  page,
}) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page
    .getByRole("button", { name: "Новое исследование", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Название", { exact: true })
    .fill("Память исследовательских приемов");
  await dialog
    .getByLabel("Точное утверждение")
    .fill("Арифметическое утверждение с явными условиями");
  await dialog.getByLabel("Область и предпосылки").fill("Натуральные числа");
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  await page.getByRole("tab", { name: /Задания/ }).click();
  const memory = page.getByRole("region", { name: "Память исследования" });
  await memory
    .getByRole("button", { name: "Добавить запись", exact: true })
    .click();
  const form = memory.getByRole("form", { name: "Запись в память" });
  await form
    .getByLabel("Содержание", { exact: true })
    .fill(
      "Проверить замену представления и отдельно доказать эквивалентность.",
    );
  await form
    .getByLabel("Условия применимости")
    .fill("Предложение для проверки; не является доказательством.");
  await form.getByRole("button", { name: "Записать", exact: true }).click();
  await expect(form).not.toBeVisible();
  await expect(memory).toContainText("Непроверенная заметка");
  await memory.getByLabel("Поиск в памяти").fill("эквивалентность");
  await expect(memory).toContainText("Проверить замену представления");
  await memory.getByLabel("Поиск в памяти").fill("не существующая запись");
  await expect(memory).toContainText("Записей не найдено.");
  await memory.getByLabel("Поиск в памяти").fill("");
  await expect(memory).toContainText("Непроверенная заметка");
  if (
    await page
      .getByRole("button", { name: "Закрыть сведения", exact: true })
      .isVisible()
  )
    await page
      .getByRole("button", { name: "Закрыть сведения", exact: true })
      .click();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await memory.scrollIntoViewIfNeeded();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    expect(
      await memory
        .locator(".team-row")
        .evaluateAll((nodes) =>
          nodes.some((n) => n.scrollWidth > n.clientWidth + 1),
        ),
    ).toBeFalsy();
    await page.screenshot({
      path: `test-results/memory-${width}.png`,
      fullPage: true,
    });
  }
  await page.reload();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page
    .getByRole("button", {
      name: "Память исследовательских приемов",
      exact: true,
    })
    .click();
  await page.getByRole("tab", { name: /Задания/ }).click();
  await expect(
    page.getByRole("region", { name: "Память исследования" }),
  ).toContainText("Непроверенная заметка");
  expect(errors).toEqual([]);
});
