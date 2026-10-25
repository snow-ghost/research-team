import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("навык сохраняет процедуру и проверяет форму без запроса модели", async ({
  page,
}) => {
  const headers = { Authorization: "Bearer " + ACCESS_KEY };
  const snapshot = async () =>
    (await (await page.request.get("/api/bootstrap", { headers })).json())
      .state;
  let state = await snapshot();
  let response = await page.request.post("/api/actions", {
    headers,
    data: {
      type: "CREATE_STUDY",
      title: "Испытание формы навыка",
      statement: "True",
      assumptions: "Prop",
      expected_revision: state.revision,
      request_id: crypto.randomUUID(),
    },
  });
  expect(response.ok()).toBeTruthy();
  state = (await response.json()).state;
  const target = state.studies.at(-1).goal;
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.getByRole("button", { name: "Исполнители", exact: true }).click();
  const registry = page.getByRole("region", {
    name: "Каталог навыков",
    exact: true,
  });
  await registry.getByRole("button", { name: "Создать версию навыка" }).click();
  const form = registry.getByRole("form", { name: "Создание навыка" });
  for (const [label, value] of [
    ["Имя", "browser-typed-output"],
    ["Название", "Проверка исходника"],
    ["Инструкции", "Верни один блок Lean"],
    ["Условия применения", "Открытая постановка"],
    ["Входные данные", "Постановка"],
    ["Критерии остановки", "Готовый исходник или предел"],
    ["Формат результата", "Один блок Lean"],
    ["Порядок действий", "Сверить цель\nПодготовить исходник"],
  ])
    await form.getByLabel(label, { exact: true }).fill(value);
  await form
    .getByRole("checkbox", { name: "Исходник Lean", exact: true })
    .check();
  await form
    .getByRole("checkbox", { name: "Доказательство", exact: true })
    .check();
  await form.getByLabel("Подтверждаю новую версию").check();
  await form.getByRole("button", { name: "Записать навык" }).click();
  await expect(form).not.toBeVisible();
  const trial = page.getByRole("form", { name: "Проверка навыка" });
  await trial
    .getByRole("combobox", { name: "Навык", exact: true })
    .selectOption("browser-typed-output@1");
  await trial
    .getByRole("combobox", { name: "Утверждение", exact: true })
    .selectOption(target);
  await trial.getByLabel("Образец результата").fill("Нет исходного файла");
  const before = await snapshot();
  await trial.getByRole("button", { name: "Проверить образец" }).click();
  const section = page.getByRole("region", {
    name: "Испытание навыка",
    exact: true,
  });
  await expect(section).toContainText("ошибка формы");
  await expect(section).toContainText("Математическая проверка не выполнена.");
  const after = await snapshot();
  expect(after.revision).toBe(before.revision);
  expect(after.attempts).toHaveLength(before.attempts.length);
  const skill = after.skill_revisions.find(
    (s) => s.id === "browser-typed-output@1",
  );
  expect(skill.configuration.contract.result_checks).toEqual(["lean_source"]);
  expect(skill.configuration.contract.procedure).toHaveLength(2);
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await section.scrollIntoViewIfNeeded();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: `test-results/skill-trial-${width}.png`,
      fullPage: true,
    });
  }
});

test("каталог показывает ограничения и требует доступный модуль", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page
    .getByRole("button", { name: "Библиотека лемм", exact: true })
    .click();
  const form = page.getByRole("form", { name: "Поиск по каталогу Lean" });
  await expect(form.getByLabel("Предел лемм", { exact: true })).toHaveValue(
    "4",
  );
  await expect(
    form.getByLabel("Предел времени, с", { exact: true }),
  ).toHaveValue("90");
  await expect(
    form.getByRole("button", { name: "Проверить каталог", exact: true }),
  ).toBeDisabled();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await form.scrollIntoViewIfNeeded();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: `test-results/catalog-search-${width}.png`,
      fullPage: true,
    });
  }
});
