import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

async function signIn(page) {
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
}

test("настройка ожидания оператора сохраняется без запуска модели", async ({
  page,
}) => {
  const headers = { Authorization: "Bearer " + ACCESS_KEY };
  const snapshot = async () =>
    (await (await page.request.get("/api/bootstrap", { headers })).json())
      .state;
  let state = await snapshot();
  const title = "Ожидание оператора без приемки";
  const response = await page.request.post("/api/actions", {
    headers,
    data: {
      type: "CREATE_STUDY",
      title,
      statement: "True",
      assumptions: "Prop",
      expected_revision: state.revision,
      request_id: crypto.randomUUID(),
    },
  });
  expect(response.ok()).toBeTruthy();
  state = (await response.json()).state;
  const study = state.studies.at(-1);
  const before = state.attempts.length;
  await signIn(page);
  await page.getByRole("button", { name: title, exact: true }).click();
  await page.getByRole("tab", { name: /Задания/ }).click();
  const close = page.getByRole("button", {
    name: "Закрыть сведения",
    exact: true,
  });
  if (await close.isVisible()) await close.click();
  const checkbox = page.getByLabel(
    "Приостанавливать поиск при готовности основной цели",
    { exact: true },
  );
  await expect(checkbox).not.toBeChecked();
  page.once("dialog", (dialog) => dialog.accept());
  await checkbox.click();
  await expect(checkbox).toBeChecked();
  const after = await snapshot();
  expect(after.studies.find((s) => s.id === study.id).pause_on_ready).toBe(
    true,
  );
  expect(after.attempts).toHaveLength(before);
  expect(after.entities.find((e) => e.id === study.goal).status).toBe("open");
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await checkbox.scrollIntoViewIfNeeded();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({ path: `test-results/ready-policy-${width}.png` });
  }
});

test("цепочка лемм имеет отдельные пределы поиска", async ({ page }) => {
  await signIn(page);
  await page
    .getByRole("button", { name: "Библиотека лемм", exact: true })
    .click();
  const form = page.getByRole("form", { name: "Поиск по каталогу Lean" });
  await form.getByRole("radio", { name: "Цепочка лемм", exact: true }).check();
  await expect(
    form.getByLabel("Глубина поиска Lean", { exact: true }),
  ).toHaveValue("3");
  await expect(form.getByLabel("Предел наборов", { exact: true })).toHaveValue(
    "16",
  );
  await expect(
    form.getByLabel("Глубина поиска Lean", { exact: true }),
  ).toHaveAttribute("max", "3");
  await expect(
    form.getByLabel("Предел наборов", { exact: true }),
  ).toHaveAttribute("max", "32");
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await form.scrollIntoViewIfNeeded();
    const radio = await form
      .getByRole("radio", { name: "Цепочка лемм", exact: true })
      .boundingBox();
    expect(radio.width).toBeLessThan(32);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({ path: `test-results/chain-search-${width}.png` });
  }
});
