import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("допуск поставщика сохраняется и требует подтверждения", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.getByRole("button", { name: "Исполнители", exact: true }).click();
  const panel = page.getByRole("region", { name: "Допуск поставщиков" });
  await panel.getByText("Изменить допуск", { exact: true }).click();
  await panel
    .getByRole("combobox", { name: "Действие", exact: true })
    .selectOption("pause");
  await panel
    .getByLabel("Основание", { exact: true })
    .fill("Контрольная пауза без модельных обращений");
  await panel
    .getByLabel("Подтверждаю изменение или запись для общего адреса поставщика")
    .check();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
  }
  await panel.getByRole("button", { name: "Применить", exact: true }).click();
  await expect(panel).toContainText("1 удержаний");
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.reload();
  await page.getByRole("button", { name: "Исполнители", exact: true }).click();
  await expect(panel).toContainText("1 удержаний");
  await panel.getByText("Изменить допуск", { exact: true }).click();
  await panel
    .getByRole("combobox", { name: "Действие", exact: true })
    .selectOption("resume");
  await panel
    .getByLabel("Основание", { exact: true })
    .fill("Отдельное разрешение новых передач");
  await panel
    .getByLabel("Подтверждаю изменение или запись для общего адреса поставщика")
    .check();
  await panel.getByRole("button", { name: "Применить", exact: true }).click();
  await expect(panel).toContainText("0 удержаний");
});
