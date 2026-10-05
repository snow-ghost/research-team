import { test, expect } from "@playwright/test";
import fs from "node:fs";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("диагностика видна и скачивается без постановки и результата модели", async ({
  page,
}) => {
  const headers = { Authorization: "Bearer " + ACCESS_KEY };
  const snapshot = async () =>
    (await page.request.get("/api/bootstrap", { headers })).json();
  const action = async (body) => {
    const current = await snapshot();
    const response = await page.request.post("/api/actions", {
      headers,
      data: {
        ...body,
        expected_revision: current.state.revision,
        request_id: crypto.randomUUID(),
      },
    });
    expect(response.ok(), await response.text()).toBeTruthy();
    return snapshot();
  };
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
    .fill("Проверка интерфейса диагностики");
  await dialog
    .getByLabel("Точное утверждение")
    .fill("Учебное утверждение для отображения результата.");
  await dialog.getByLabel("Область и предпосылки").fill("Искусственные данные");
  await dialog.getByRole("button", { name: "Подтвердить" }).click();
  let view = await snapshot();
  const goal = view.state.studies.find(
    (s) => s.title === "Проверка интерфейса диагностики",
  ).goal;
  view = await action({
    type: "TASK",
    target: goal,
    title: "Результат для отображения диагностики",
    kind: "proof",
    text: "Верни учебный результат.",
  });
  const task = view.state.tasks.find((t) => t.target === goal);
  const response = await page.request.post("/api/attempts", {
    headers,
    data: {
      task_id: task.id,
      profile: "fixture-proof",
      workspace: "example",
      confirm: true,
      expected_revision: view.state.revision,
      request_id: crypto.randomUUID(),
    },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  await expect
    .poll(
      async () =>
        (await snapshot()).state.attempts.find((a) => a.task_id === task.id)
          ?.status,
    )
    .toBe("candidate");
  view = await snapshot();
  const attempt = view.state.attempts.find((a) => a.task_id === task.id);
  const original = await (
    await page.request.get("/api/attempts/" + attempt.id, { headers })
  ).json();
  // The RPC fixture tests server capture; this fixture isolates rendering and export.
  await page.route("**/api/attempts/" + attempt.id, (route) =>
    route.fulfill({
      json: {
        ...original,
        status: "failed",
        candidate: undefined,
        failure_code: "protocol_or_remote_error",
        diagnostics: [
          {
            source: "acp",
            stage: "prompt",
            kind: "rpc_error",
            rpc_code: -32603,
            message: "LLM error: fixture provider unavailable [REDACTED]",
            elapsed_ms: 90000,
            data: { http_status: 503, request_id: "fixture-request" },
          },
          {
            source: "process",
            stage: "process",
            kind: "process_exit",
            exit_code: -1,
            signal: "killed",
            client_stop_requested: true,
            byte_limit_reached: true,
          },
        ],
      },
    }),
  );
  await page.getByRole("tab", { name: /Задания/ }).click();
  await page
    .locator(".attempt-row")
    .filter({ hasText: attempt.id })
    .getByRole("button", { name: "Результат", exact: true })
    .click();
  const diagnostic = page.getByRole("region", {
    name: "Диагностика исполнителя",
    exact: true,
  });
  await expect(diagnostic).toContainText("-32603");
  await expect(diagnostic).toContainText("fixture provider unavailable");
  await expect(diagnostic).toContainText(
    "Достигнут предел байтов вывода процесса.",
  );
  const [download] = await Promise.all([
    page.waitForEvent("download"),
    diagnostic
      .getByRole("button", { name: "Скачать диагностику", exact: true })
      .click(),
  ]);
  const file = "test-results/diagnostics.json";
  await download.saveAs(file);
  const report = JSON.parse(fs.readFileSync(file, "utf8"));
  expect(report.format).toBe("research-team-execution-diagnostics-v1");
  expect(report.diagnostics[0].rpc_code).toBe(-32603);
  expect(report.diagnostics[1].byte_limit_reached).toBe(true);
  expect(report.candidate).toBeUndefined();
  expect(report.task).toBeUndefined();
  expect(report.input).toBeUndefined();
  await page
    .getByRole("button", { name: "Закрыть сведения", exact: true })
    .click();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await diagnostic.scrollIntoViewIfNeeded();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: "test-results/diagnostics-" + width + ".png",
      fullPage: true,
    });
  }
  expect(errors).toEqual([]);
});
