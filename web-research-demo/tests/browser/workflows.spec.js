import { test, expect } from "@playwright/test";

test("маршруты связей обходят узлы и заканчиваются у цели", async ({ page }) => {
  for (const mode of ["Ветви исследования", "Зависимости"]) {
    await page.getByRole("button", { name: mode, exact: true }).click();
    await expect(page.locator('[data-layout="ready"]')).toBeVisible();
    await page.waitForTimeout(350);
    const issues = await page.evaluate(() => {
      const result = [];
      const nodes = [...document.querySelectorAll(".react-flow__node")].map(node => ({
        id: node.getAttribute("data-id"), rect: node.getBoundingClientRect(),
      }));
      for (const path of document.querySelectorAll(".react-flow__edge-path")) {
        const id = path.closest(".react-flow__edge").getAttribute("data-id");
        const [source, target] = id.split(":");
        const length = path.getTotalLength();
        const matrix = path.getScreenCTM();
        for (let at = 0; at <= length; at += 3) {
          const p = path.getPointAtLength(at);
          const point = new DOMPoint(p.x, p.y).matrixTransform(matrix);
          for (const node of nodes) {
            if ([source, target].includes(node.id)) continue;
            const r = node.rect;
            if (point.x > r.left + 2 && point.x < r.right - 2 &&
                point.y > r.top + 2 && point.y < r.bottom - 2) {
              result.push(id + " crosses " + node.id);
              break;
            }
          }
        }
        const p = path.getPointAtLength(length);
        const end = new DOMPoint(p.x, p.y).matrixTransform(matrix);
        const rect = nodes.find(node => node.id === target).rect;
        if (Math.abs(end.y - rect.top) > 6 || end.x < rect.left || end.x > rect.right)
          result.push(id + " misses target");
        const before = path.getPointAtLength(Math.max(0,length - 2));
        if (p.y < before.y) result.push(id + " reverses direction");
      }
      return [...new Set(result)];
    });
    expect(issues).toEqual([]);
  }
});


const state = (page) =>
  page.evaluate(() =>
    JSON.parse(localStorage.getItem("research-team-demo-v1")),
  );
test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto("/");
  await expect(page.locator('[data-layout="ready"]')).toBeVisible();
});
test("desktop: карта, зависимости, стрелки и размеры", async ({ page }) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await expect(page.locator(".research-node")).toHaveCount(5);
  await page.waitForTimeout(500);
  const geometry = await page.evaluate(() => {
    const nodes = [...document.querySelectorAll(".react-flow__node")].map(
      (el) => el.getBoundingClientRect(),
    );
    const overlaps = nodes.flatMap((a, i) =>
      nodes
        .slice(i + 1)
        .filter(
          (b) =>
            Math.min(a.right, b.right) - Math.max(a.left, b.left) > 1 &&
            Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 1,
        ),
    ).length;
    const paths = [...document.querySelectorAll(".react-flow__edge-path")];
    return {
      overlaps,
      lengths: paths.map((p) => p.getTotalLength()),
      markers: paths.every((p) =>
        p.getAttribute("marker-end")?.includes("url("),
      ),
      overflow: document.documentElement.scrollWidth > innerWidth,
    };
  });
  expect(geometry.overlaps).toBe(0);
  expect(geometry.lengths.every((x) => x > 10)).toBeTruthy();
  expect(geometry.markers).toBeTruthy();
  expect(geometry.overflow).toBeFalsy();
  await page.screenshot({ path: "test-results/desktop.png", fullPage: true });
  await page.getByRole("button", { name: "Зависимости", exact: true }).click();
  await expect(page.locator('[data-layout="ready"]')).toBeVisible();
  await expect(page.locator(".rule-node").first()).toContainText("Обоснование");
  await page.waitForTimeout(350);
  await page.screenshot({
    path: "test-results/dependencies.png",
    fullPage: true,
  });
  expect(errors).toEqual([]);
});
test("вопрос привязан к объекту и переживает перезагрузку", async ({
  page,
}) => {
  await page.locator('[data-entity="H1"]').click();
  await page.getByRole("button", { name: "Вопросы", exact: true }).click();
  await page
    .getByLabel("Сообщение к H1@1")
    .fill("Какие условия нужны для индукции?");
  await page.getByRole("button", { name: "Отправить", exact: true }).click();
  expect((await state(page)).questions[0].target).toBe("H1");
  expect((await state(page)).questions[0].snapshot).toBe(1);
  await page.reload();
  await page.locator('[data-entity="H1"]').click();
  await page.getByRole("button", { name: "Вопросы", exact: true }).click();
  await expect(
    page.getByText("Какие условия нужны для индукции?", { exact: true }),
  ).toBeVisible();
});
test("разбиение создает обязательство покрытия, но не закрывает цель", async ({
  page,
}) => {
  await page.locator('[data-entity="H2"]').click();
  await page
    .getByRole("button", { name: "Разбить на случаи", exact: true })
    .click();
  await page.getByLabel("Первый случай").fill("Одна компонента");
  await page.getByLabel("Второй случай").fill("Несколько компонент");
  await page.getByRole("button", { name: "Подтвердить", exact: true }).click();
  const s = await state(page);
  expect(
    s.entities.some((x) => x.title === "Покрытие нового разбиения"),
  ).toBeTruthy();
  expect(s.entities.find((x) => x.id === "H2").status).toBe("open");
  expect(s.entities.find((x) => x.id === "H0").status).toBe("open");
});
test("общая лемма используется через непроверенный переход", async ({
  page,
}) => {
  await page
    .getByRole("button", { name: "Библиотека лемм", exact: true })
    .click();
  await page.getByRole("button", { name: "Применить L2", exact: true }).click();
  await page.getByLabel("Целевая гипотеза").selectOption("H3");
  await page.getByRole("button", { name: "Подтвердить", exact: true }).click();
  const s = await state(page);
  expect(s.applications).toHaveLength(1);
  expect(s.entities.find((x) => x.id === s.applications[0].id).status).toBe(
    "in_review",
  );
  expect(s.entities.find((x) => x.id === "H3").status).toBe("open");
});
test("замечание блокирует приемку и не закрывается ответом автора", async ({
  page,
}) => {
  await page.locator('[data-entity="H1"]').click();
  await page.getByRole("button", { name: "Основания", exact: true }).click();
  await page
    .getByRole("button", { name: "Добавить замечание", exact: true })
    .click();
  await page.getByLabel("Категория").selectOption("major");
  await page
    .getByLabel("Основание замечания")
    .fill("Нужна отдельная проверка базового случая.");
  await page.getByRole("button", { name: "Подтвердить", exact: true }).click();
  await page
    .getByRole("button", { name: "Принять кандидат", exact: true })
    .click();
  await page.getByRole("button", { name: "Подтвердить", exact: true }).click();
  await expect(page.getByRole("status")).toContainText(
    "незакрытое математическое замечание",
  );
  await page.getByRole("button", { name: "Отмена", exact: true }).click();
  await page.getByRole("button", { name: /^Рецензирование/ }).click();
  await page
    .getByRole("button", { name: "Запросить перепроверку", exact: true })
    .click();
  expect((await state(page)).findings[0].state).toBe("verification_pending");
});
test("оспаривание леммы и исторический снимок", async ({ page }) => {
  await page.locator('[data-entity="L1"]').click();
  await page
    .getByRole("button", { name: "Оспорить основание", exact: true })
    .click();
  await page.getByLabel("Основание замечания").fill("Уточнить границу n >= 2.");
  await page.getByRole("button", { name: "Подтвердить", exact: true }).click();
  await expect(page.locator('[data-entity="H0"] .badge')).toContainText(
    "Зависимость оспорена",
  );
  await page
    .getByRole("button", { name: "Журнал действий", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Открыть снимок", exact: true })
    .last()
    .click();
  await expect(
    page.getByText("Исторический снимок v1. Только чтение.", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Предложить лемму", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Сбросить макет", exact: true }),
  ).toBeDisabled();
  await expect(page.locator('[data-entity="L1"] .badge')).toHaveText("Принято");
  await page.getByRole("button", { name: "К текущему состоянию" }).click();
  await expect(page.locator('[data-entity="L1"] .badge')).toHaveText(
    "Оспорено",
  );
});
test("пауза, три события и выгрузка снимка", async ({ page }) => {
  await page
    .getByRole("button", { name: "Приостановить", exact: true })
    .click();
  await expect(
    page.getByRole("button", {
      name: "Следующее событие сценария",
      exact: true,
    }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Продолжить", exact: true }).click();
  for (let i = 0; i < 3; i++)
    await page
      .getByRole("button", { name: "Следующее событие сценария", exact: true })
      .click();
  expect((await state(page)).scenarioStep).toBe(3);
  expect((await state(page)).entities.find((x) => x.id === "H3").status).toBe(
    "open",
  );
  const download = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "Выгрузить снимок", exact: true })
    .click();
  expect((await download).suggestedFilename()).toMatch(/research-team-demo-v/);
});
test("мобильный экран: список, сведения, вопрос и отсутствие переполнения", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await expect(page.getByText("Макет", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("region", { name: "Сведения об объекте" }),
  ).toHaveCount(0);
  await expect(page.locator(".claim-list")).toBeVisible();
  await expect(page.locator(".inspector")).toHaveCount(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
  ).toBeFalsy();
  await page.screenshot({ path: "test-results/mobile.png", fullPage: true });
  await page
    .getByRole("button", { name: /Подгипотеза H1 Связный случай/ })
    .click();
  await expect(page.locator(".inspector")).toBeVisible();
  await page.getByRole("button", { name: "Вопросы", exact: true }).click();
  await page
    .getByLabel("Сообщение к H1@1")
    .fill("Что известно о базовом случае?");
  await page.getByRole("button", { name: "Отправить", exact: true }).click();
  await expect(
    page.getByText("Что известно о базовом случае?", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Закрыть сведения", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Ветви исследования", exact: true })
    .click();
  await expect(page.locator('[data-layout="ready"]')).toBeVisible();
  await page.waitForTimeout(300);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
  ).toBeFalsy();
  await page.screenshot({
    path: "test-results/mobile-graph.png",
    fullPage: true,
  });
});
test("очень узкий экран и длинный пользовательский заголовок", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 760 });
  await page.reload();
  await page
    .getByRole("button", { name: "Предложить лемму", exact: true })
    .click();
  await page
    .getByLabel("Название", { exact: true })
    .fill(
      "Длинное название вспомогательной леммы о сохранении условий применения при разбиении на компоненты",
    );
  await page
    .getByLabel("Точное утверждение")
    .fill("Учебное предложение для проверки отображения.");
  await page.getByLabel("Область и предпосылки").fill("Конечные графы.");
  await page.getByRole("button", { name: "Подтвердить", exact: true }).click();
  await expect(page.locator(".inspector h2")).toContainText("Длинное название");
  const rect = await page
    .locator(".inspector h2")
    .evaluate((el) => ({ height: el.scrollHeight, box: el.clientHeight }));
  expect(rect.height).toBeLessThanOrEqual(rect.box + 1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
  ).toBeFalsy();
});
