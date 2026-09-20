import { test, expect } from "@playwright/test";
import { ACCESS_KEY, SOURCE_COMMIT } from "../fixtures/constants.js";

async function data(page) {
  const response = await page.request.get("/api/bootstrap", {
    headers: { Authorization: "Bearer " + ACCESS_KEY },
  });
  expect(response.ok()).toBeTruthy();
  return response.json();
}
async function command(page, action) {
  const snapshot = await data(page);
  const response = await page.request.post("/api/actions", {
    headers: { Authorization: "Bearer " + ACCESS_KEY },
    data: {
      ...action,
      expected_revision: snapshot.state.revision,
      request_id: crypto.randomUUID(),
    },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  return response.json();
}
test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await expect(page.getByText("Серверная рабочая область")).toBeVisible();
});

test("исследование, реальный запуск через API, приемка и применение леммы", async ({
  page,
}) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page
    .getByRole("button", { name: "Новое исследование", exact: true })
    .click();
  let dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Название", { exact: true })
    .fill("Исследование через API");
  await dialog
    .getByLabel("Точное утверждение")
    .fill("Проверить общее утверждение");
  await dialog.getByLabel("Область и предпосылки").fill("Конечная область");
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name: "Исследование через API" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Предложить лемму", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Название", { exact: true })
    .fill("Лемма для повторного применения");
  await dialog
    .getByLabel("Точное утверждение")
    .fill("Промежуточное утверждение");
  await dialog.getByLabel("Область и предпосылки").fill("Конечная область");
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  await expect(dialog).not.toBeVisible();
  let current = await data(page);
  const lemma = current.state.entities.find(
    (e) => e.title === "Лемма для повторного применения",
  );
  const goal = current.state.studies.find(
    (s) => s.title === "Исследование через API",
  ).goal;
  await page.getByRole("tab", { name: /Задания/ }).click();
  await page.getByRole("button", { name: "Новое задание" }).click();
  await page.getByRole("combobox", { name: "Утверждение", exact: true }).selectOption(lemma.id);
  await page
    .getByLabel("Название", { exact: true })
    .fill("Доказать лемму через исполнителя");
  await page
    .getByLabel("Цель", { exact: true })
    .fill("Представить кандидат доказательства.");
  await page.getByRole("button", { name: "Создать задание" }).click();
  await page
    .getByRole("button", {
      name: "Настроить запуск Доказать лемму через исполнителя",
    })
    .click();
  await page
    .getByRole("combobox", { name: "Исполнитель", exact: true })
    .selectOption("fixture-reader");
  await page
    .getByLabel("Подтверждаю запуск и возможные расходы модели")
    .check();
  await page.getByRole("button", { name: "Запустить", exact: true }).click();
  await expect(page.locator(".attempt-row").last()).toContainText("Кандидат");
  current = await data(page);
  expect(current.state.entities.find((e) => e.id === lemma.id).status).toBe(
    "open",
  );
  await page.getByRole("button", { name: "Результат", exact: true }).click();
  await expect(
    page.getByRole("region", { name: "Результат попытки" }),
  ).toContainText("Кандидат доказательства");
  await page
    .getByRole("button", { name: "Направить материал на приемку" })
    .click();
  await page.getByRole("button", { name: "Основания", exact: true }).click();
  await page
    .getByRole("button", { name: "Принять кандидат", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("Обоснование решения")
    .fill("Условия и переходы проверены оператором на учебном материале.");
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  await expect
    .poll(
      async () =>
        (await data(page)).state.entities.find((e) => e.id === lemma.id).status,
    )
    .toBe("accepted");
  await page
    .getByRole("button", { name: "Библиотека лемм", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Применить " + lemma.id, exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("Целевая гипотеза").selectOption(goal);
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  await expect
    .poll(async () => (await data(page)).state.applications.length)
    .toBe(1);
  current = await data(page);
  expect(current.state.entities.find((e) => e.id === goal).status).toBe("open");
  await page.reload();
  await expect(page.getByText("Серверная рабочая область")).toBeVisible();
  expect(await page.evaluate(() => localStorage.length)).toBe(0);
  await expect(page.locator('.graph[data-layout="ready"]')).toBeVisible();
  await expect.poll(async () => {
    const node = await page.locator(".research-node").first().boundingBox();
    const canvas = await page.locator(".graph").boundingBox();
    return node && canvas && node.x > canvas.x + 30;
  }).toBeTruthy();
  await page.screenshot({
    path: "test-results/mvp-desktop.png",
    fullPage: true,
  });
  expect(errors).toEqual([]);
});

test("разбиение, вопросы, история и узкий экран работают с серверными данными", async ({
  page,
}) => {
  let current = await command(page, {
    type: "CREATE_STUDY",
    title: "Разбиение API",
    statement: "Утверждение с двумя случаями",
    assumptions: "Явные предпосылки",
  });
  const study = current.state.studies.at(-1);
  await page
    .getByRole("button", { name: "Разбиение API", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Разбить на случаи", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Первый случай").fill("Первый случай");
  await dialog.getByLabel("Второй случай").fill("Второй случай");
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  await expect
    .poll(
      async () =>
        (await data(page)).state.entities.find((e) => e.id === study.goal)
          .dependencies.length,
    )
    .toBe(3);
  await expect(page.locator(".research-node")).toHaveCount(4);
  await page.getByRole("button", { name: "Вопросы", exact: true }).click();
  await page
    .getByLabel("Сообщение к " + study.goal + "@2", { exact: true })
    .fill("Какие случаи еще не проверены?");
  await page.getByRole("button", { name: "Отправить", exact: true }).click();
  await expect(page.getByText("Ответ еще не получен")).toBeVisible();
  await page
    .getByRole("button", { name: "Журнал действий", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Открыть снимок", exact: true })
    .first()
    .click();
  await expect(page.getByText(/Исторический снимок v/)).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Искать контрпример" }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Исполнители", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: /К текущему состоянию/ }).click();
  await page.setViewportSize({ width: 320, height: 740 });
  await page.reload();
  await expect(page.getByText("Серверная рабочая область")).not.toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth + 1,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: "test-results/mvp-mobile.png",
    fullPage: true,
  });
});

test("Coddy: подготовка, подтверждение плана и получение кандидата через Go API", async ({
  page,
}) => {
  let current = await command(page, {
    type: "CREATE_STUDY",
    title: "Coddy API",
    statement: "Проверить конечные случаи",
    assumptions: "Малые размеры",
  });
  const goal = current.state.studies.at(-1).goal;
  current = await command(page, {
    type: "TASK",
    target: goal,
    title: "Проверка через Coddy",
    kind: "proof",
    text: "Подготовить программу проверки",
  });
  const task = current.state.tasks.at(-1);
  await page.getByRole("button", { name: "Coddy Bot", exact: true }).click();
  await page
    .getByRole("button", { name: "Подготовить задание", exact: true })
    .click();
  await page.getByRole("combobox", { name: "Задание", exact: true }).selectOption(task.id);
  await page.getByLabel("Исходный хеш Git").fill(SOURCE_COMMIT);
  await page
    .getByLabel("Условия приемки")
    .fill("Воспроизводимый запуск\nПроверка граничного случая");
  await page
    .getByRole("button", { name: "Подготовить без публикации" })
    .click();
  await expect(
    page.getByText("Подготовлено локально", { exact: true }),
  ).toBeVisible();
  await page.getByRole("combobox", { name: "Действие", exact: true }).selectOption("submit");
  await page
    .getByRole("button", { name: "Опубликовать задание", exact: true })
    .click();
  await expect(page.getByRole("link", { name: "Задача #1" })).toHaveCount(0);
  await page
    .getByLabel("Подтверждаю отправку данных в GitHub и возможный запуск Coddy")
    .check();
  await page
    .getByRole("button", { name: "Опубликовать задание", exact: true })
    .click();
  await expect(page.getByRole("link", { name: "Задача #1" })).toBeVisible();
  await page.getByRole("combobox", { name: "Действие", exact: true }).selectOption("observe");
  await page
    .getByRole("button", { name: "Обновить GitHub", exact: true })
    .click();
  await expect(page.getByText("План ожидает подтверждения", { exact: true })).toBeVisible();
  await page
    .getByRole("combobox", { name: "Действие", exact: true })
    .selectOption("approve-plan");
  await page
    .getByRole("combobox", { name: "Сообщение Coddy", exact: true })
    .selectOption("issue:101");
  await page
    .getByLabel(
      "Разрешаю Coddy выполнять план, изменять код и публиковать изменения",
    )
    .check();
  await page
    .getByRole("button", { name: "Подтвердить план", exact: true })
    .click();
  await page.getByRole("combobox", { name: "Действие", exact: true }).selectOption("observe");
  await page
    .getByRole("button", { name: "Обновить GitHub", exact: true })
    .click();
  await expect(
    page.getByRole("link", { name: "Запрос на слияние #2" }),
  ).toBeVisible();
  await page.getByRole("combobox", { name: "Действие", exact: true }).selectOption("collect");
  await page
    .getByRole("button", { name: "Собрать материалы", exact: true })
    .click();
  await expect(page.getByRole("button", { name: "Собрать материалы", exact: true })).toBeEnabled();
  await expect(page.getByRole("alert")).toHaveCount(0);
  const id = (await data(page)).state.delegations.at(-1).id;
  const record = await page.request
    .get("/api/coddy/" + id, {
      headers: { Authorization: "Bearer " + ACCESS_KEY },
    })
    .then((r) => r.json());
  expect(record.candidate_sha256).toHaveLength(1);
  const candidate = await page.request
    .get("/api/coddy/" + id + "/candidates/" + record.candidate_sha256[0], {
      headers: { Authorization: "Bearer " + ACCESS_KEY },
    })
    .then((r) => r.json());
  expect(candidate.status).toBe("candidate");
  expect(candidate.checks_executed).toBe(false);
  expect(
    (await data(page)).state.entities.find((e) => e.id === goal).status,
  ).toBe("open");
  await page.screenshot({ path: "test-results/mvp-coddy-desktop.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Закрыть сведения", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Coddy Bot", level: 1 })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBeTruthy();
  await page.screenshot({ path: "test-results/mvp-coddy-mobile.png", fullPage: true });
});
