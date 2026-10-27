import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("просмотр разделяет проверку Lean и приемку без изменения журнала", async ({
  page,
}) => {
  test.skip(!process.env.MVP_LEAN_CONFIG, "Requires the local Lean checker");
  const headers = { Authorization: "Bearer " + ACCESS_KEY };
  const snapshot = async () =>
    (await page.request.get("/api/bootstrap", { headers })).json();
  const command = async (route, body) => {
    const before = await snapshot();
    const response = await page.request.post("/api" + route, {
      headers,
      data: {
        ...body,
        expected_revision: before.state.revision,
        request_id: "inspect-" + crypto.randomUUID(),
      },
    });
    expect(response.ok()).toBeTruthy();
  };
  await command("/actions", {
    type: "CREATE_STUDY",
    title: "Просмотр проверенного файла",
    statement: "Истинное утверждение",
    assumptions: "Prop",
  });
  const created = await snapshot();
  const target = created.state.studies.find(
    (s) => s.title === "Просмотр проверенного файла",
  ).goal;
  const study = created.state.studies.find((s) => s.goal === target).id;
  await command("/actions", {
    type: "LEMMA",
    study,
    title: "Дополнительная цель",
    statement: "Отдельная цель без доказательства",
    assumptions: "Prop",
  });
  const other = (await snapshot()).state.entities.find(
    (e) => e.study === study && e.title === "Дополнительная цель",
  ).id;
  await command(`/entities/${target}/formal-goal`, {
    goal: {
      source: "def Statement : Prop := True\n",
      declaration: "Statement",
      candidate: "Candidate",
    },
  });
  await command(`/entities/${target}/proof-source`, {
    source: "import Goal\ntheorem Candidate : Statement := by trivial\n",
    author: "Fixture",
    basis: "Местный учебный файл для проверки интерфейса",
  });
  await expect
    .poll(
      async () =>
        (await snapshot()).state.verifications.find((v) => v.target === target)
          ?.status,
      { timeout: 30000 },
    )
    .toBe("verified");
  const before = await snapshot(),
    errors = [],
    writes = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("request", (r) => {
    if (
      r.url().includes("/api/") &&
      !r.url().endsWith("/api/session") &&
      r.method() !== "GET"
    )
      writes.push(r.url());
  });
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page
    .getByRole("button", { name: "Просмотр проверенного файла", exact: true })
    .click();
  await page.getByRole("tab", { name: /Задания/ }).click();
  await page
    .getByRole("button", { name: "Закрыть сведения", exact: true })
    .click();
  const panel = page.getByRole("region", {
    name: "Проверка утверждения",
    exact: true,
  });
  await expect(
    panel.getByRole("heading", { name: "Постановка", exact: true }),
  ).toBeVisible();
  await expect(panel.getByLabel("Состояние проверок")).toContainText(
    "Решение оператораОткрыто",
  );
  await expect(
    panel.getByRole("region", {
      name: "Фактический исходник доказательства",
      exact: true,
    }),
  ).toContainText("theorem Candidate");
  await expect(panel).toContainText("SHA256 байтов:");
  const download = page.waitForEvent("download");
  await panel
    .getByRole("button", { name: "Скачать Candidate.lean", exact: true })
    .click();
  expect((await download).suggestedFilename()).toBe("Candidate.lean");
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await panel
      .getByLabel("Утверждение для проверки", { exact: true })
      .selectOption(other);
    await expect(
      panel.getByText("Отдельная цель без доказательства", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("complementary", {
        name: "Сведения об объекте",
        exact: true,
      }),
    ).toBeHidden();
    await panel
      .getByLabel("Утверждение для проверки", { exact: true })
      .selectOption(target);
    await expect(
      panel.getByRole("region", {
        name: "Фактический исходник доказательства",
        exact: true,
      }),
    ).toContainText("theorem Candidate");
    for (const name of ["Файлы", "Проверки", "Зависимости", "Расход"]) {
      await panel.getByRole("tab", { name, exact: true }).click();
      await expect(
        panel.getByRole("tab", { name, exact: true }),
      ).toHaveAttribute("aria-selected", "true");
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      ).toBeTruthy();
    }
    await panel.getByRole("tab", { name: "Файлы", exact: true }).click();
    await panel.scrollIntoViewIfNeeded();
    await page.screenshot({
      path: `test-results/proof-inspection-${width}.png`,
    });
  }
  expect(errors).toEqual([]);
  expect(writes).toEqual([]);
  const after = await snapshot();
  expect(after.state.revision).toBe(before.state.revision);
  expect(after.state.attempts).toHaveLength(before.state.attempts.length);
  expect(after.state.entities.find((e) => e.id === target).status).toBe("open");
});
