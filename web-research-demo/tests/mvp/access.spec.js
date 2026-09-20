import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("вход сообщает точную ошибку и допускает пробелы вокруг ключа", async ({
  page,
}) => {
  await page.goto("/");
  const input = page.getByLabel("Ключ доступа");
  await input.fill("incorrect-access-key");
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText("Неверный ключ.");
  await expect(page.getByText("Серверная рабочая область")).toHaveCount(0);

  await input.fill("  " + ACCESS_KEY + "  ");
  const request = page.waitForRequest(
    (r) => r.url().endsWith("/api/session") && r.method() === "POST",
  );
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  expect((await request).postDataJSON().token).toBe(ACCESS_KEY);
  await expect(page.getByText("Серверная рабочая область")).toBeVisible();
  expect(await page.evaluate(() => localStorage.length)).toBe(0);
  await page.getByRole("button", { name: "Выйти", exact: true }).click();
  await expect(input).toBeVisible();
});
