import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("отдельный файл проходит Lean и получает точную рецензию", async ({
  page,
}) => {
  test.skip(
    !process.env.MVP_LEAN_CONFIG,
    "MVP_LEAN_CONFIG enables the real Lean checker",
  );
  const headers = { Authorization: "Bearer " + ACCESS_KEY };
  const snapshot = async () =>
    (await page.request.get("/api/bootstrap", { headers })).json();
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
    .fill("Регистрация отдельного файла Lean");
  await dialog
    .getByLabel("Точное утверждение")
    .fill("Истинное учебное утверждение");
  await dialog.getByLabel("Область и предпосылки").fill("Prop");
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  let view = await snapshot();
  const target = view.state.studies.find(
    (s) => s.title === "Регистрация отдельного файла Lean",
  ).goal;
  const initialAttempts = view.state.attempts.length;
  await page.getByRole("tab", { name: /Задания/ }).click();
  await page
    .getByRole("button", { name: "Закрепить цель Lean", exact: true })
    .click();
  await page.getByLabel("Имя цели", { exact: true }).fill("Statement");
  await page.getByLabel("Имя теоремы кандидата").fill("Candidate");
  await page
    .getByLabel("Исходный текст Goal.lean")
    .fill("def Statement : Prop := True\n");
  await page
    .getByRole("button", { name: "Закрепить версию", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Зарегистрировать файл Lean", exact: true })
    .click();
  const form = page.getByRole("form", { name: "Регистрация доказательства" });
  await form.getByLabel("Автор", { exact: true }).fill("Codex");
  await form
    .getByLabel("Происхождение", { exact: true })
    .fill("Учебный файл, подготовленный отдельно.");
  await form
    .getByLabel("Исходный текст Candidate.lean")
    .fill("import Goal\ntheorem Candidate : Statement := by trivial\n");
  await form
    .getByRole("button", { name: "Зарегистрировать и проверить", exact: true })
    .click();
  await expect
    .poll(
      async () =>
        (await snapshot()).state.verifications.find((v) => v.target === target)
          ?.status,
      { timeout: 30000 },
    )
    .toBe("verified");
  const row = page
    .locator(".verification-row")
    .filter({ hasText: "Проверено" });
  await row.locator(":scope > summary").click();
  await expect(row).toContainText("Автор: Codex");
  view = await snapshot();
  expect(view.state.attempts).toHaveLength(initialAttempts);
  const verification = view.state.verifications.find(
    (v) => v.target === target && v.status === "verified",
  );
  const taskResponse = await page.request.post("/api/actions", {
    headers,
    data: {
      type: "TASK",
      target,
      kind: "review",
      title: "Точная рецензия зарегистрированного файла",
      text: "Проверь переданный проверенный исходник",
      expected_revision: view.state.revision,
      request_id: "typed-review-" + Date.now(),
    },
  });
  expect(taskResponse.ok()).toBeTruthy();
  await page
    .getByRole("button", {
      name: "Настроить запуск Точная рецензия зарегистрированного файла",
      exact: true,
    })
    .click();
  const reviewForm = page.getByRole("form", { name: "Запуск задания" });
  await reviewForm
    .getByLabel("Проверенный исходник", { exact: true })
    .selectOption(verification.id);
  await reviewForm
    .getByLabel("Исполнитель", { exact: true })
    .selectOption("fixture-review");
  await reviewForm.getByRole("checkbox", { name: /Подтверждаю/ }).check();
  await reviewForm
    .getByRole("button", { name: "Запустить", exact: true })
    .click();
  await expect
    .poll(
      async () =>
        (await snapshot()).state.attempts.find(
          (a) => a.proof_binding?.verification === verification.id,
        )?.status,
      { timeout: 15000 },
    )
    .toBe("candidate");
  view = await snapshot();
  const reviewed = view.state.attempts.find(
    (a) => a.proof_binding?.verification === verification.id,
  );
  expect(reviewed.proof_binding.source_sha256).toBe(
    verification.report.source_sha256,
  );
  expect(view.state.entities.find((e) => e.id === target).status).toBe("open");
  await expect(
    page.locator(".attempt-row").filter({ hasText: reviewed.id }),
  ).toContainText(/кандидат/i);
  const [attachment] = await Promise.all([
    page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/verifications/" + verification.id + "/attach") &&
        r.request().method() === "POST",
    ),
    row
      .getByRole("button", { name: "Направить на приемку", exact: true })
      .click(),
  ]);
  expect(attachment.ok()).toBeTruthy();
  view = await snapshot();
  const entity = view.state.entities.find((e) => e.id === target);
  expect(entity.status).toBe("in_review");
  expect(entity.proofAuthor).toBe("Codex");
  expect(entity.proofAttempt).toBeUndefined();
  expect(view.state.attempts).toHaveLength(initialAttempts + 1);
  await page.reload();
  await page
    .getByRole("button", {
      name: "Регистрация отдельного файла Lean",
      exact: true,
    })
    .click();
  await page.getByRole("tab", { name: /Задания/ }).click();
  const restored = page
    .locator(".verification-row")
    .filter({ hasText: "Проверено" });
  await restored.locator(":scope > summary").click();
  await restored.getByText("Исходный текст", { exact: true }).click();
  await expect(restored).toContainText("theorem Candidate");
  await expect(
    restored.getByRole("button", { name: "Направить на приемку", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Основания", exact: true }).click();
  const inspector = page.getByRole("complementary", {
    name: "Сведения об объекте",
  });
  await expect(inspector).toContainText("Автор: Codex");
  await expect(inspector).toContainText("Lean: подтверждено для версии 3.");
  await expect(inspector.locator(".proof-source")).toContainText(
    "theorem Candidate",
  );
  await page
    .getByRole("button", { name: "Закрыть сведения", exact: true })
    .click();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await restored.scrollIntoViewIfNeeded();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: "test-results/submitted-proof-" + width + ".png",
      fullPage: true,
    });
  }
  expect(errors).toEqual([]);
});
