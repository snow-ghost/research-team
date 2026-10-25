import { test, expect } from "@playwright/test";
import { ACCESS_KEY } from "../fixtures/constants.js";

test("история сравнивает точные версии и решение о разбиении удерживает ветвь", async ({
  page,
}) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const headers = { Authorization: "Bearer " + ACCESS_KEY };
  const bootstrap = async () =>
    await (await page.request.get("/api/bootstrap", { headers })).json();
  const command = async (route, body) => {
    const { state } = await bootstrap();
    const response = await page.request.post("/api" + route, {
      headers,
      data: {
        ...body,
        expected_revision: state.revision,
        request_id: crypto.randomUUID(),
      },
    });
    expect(response.ok(), await response.text()).toBeTruthy();
    return (await response.json()).state;
  };
  let { state, lean } = await bootstrap();
  const wasPaused = state.paused;
  state = await command("/actions", {
    type: "CREATE_STUDY",
    title: "История и решения по ветвям",
    statement: "True",
    assumptions: "Prop",
  });
  const study = state.studies.at(-1);
  if (!wasPaused) await command("/actions", { type: "PAUSE" });
  const goal = {
    source: "def HistoryBrowser : Prop := True\n",
    declaration: "HistoryBrowser",
    candidate: "historyBrowserProof",
  };
  state = await command(`/entities/${study.goal}/formal-goal`, { goal });
  const from = state.revision;
  state = await command(`/entities/${study.goal}/formal-goal`, {
    goal: { ...goal, source: goal.source + "-- NEW-GOAL-VERSION\n" },
  });
  const to = state.revision;
  let branch;
  if (lean.configured) {
    state = await command("/branches", {
      method: "induction",
      rationale: "Отдельно проверить условия разбиения",
      priority: 10,
      confirm: true,
      team: {
        study: study.id,
        target: study.goal,
        profiles: {
          proof: "fixture-proof",
          counterexample: "fixture-counterexample",
          formalize: "fixture-proof",
          review: "fixture-review",
        },
        workspace: "example",
        max_attempts: 6,
        require_lean: true,
      },
    });
    branch = state.branches.at(-1);
  }
  try {
    await page.goto("/");
    await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await page.getByRole("button", { name: study.title, exact: true }).click();
    await page
      .getByRole("button", { name: "Журнал действий", exact: true })
      .click();
    const history = page.getByRole("region", {
      name: "История исследования",
      exact: true,
    });
    await history
      .getByLabel("Исходный снимок", { exact: true })
      .fill(String(from));
    await history
      .getByLabel("Конечный снимок", { exact: true })
      .fill(String(to));
    await history
      .getByRole("button", { name: "Сравнить версии", exact: true })
      .click();
    const diff = page.getByRole("region", { name: "Различия версий" });
    await expect(diff).toContainText(`Снимки ${from} и ${to}`);
    await diff.locator("summary").click();
    await expect(diff).toContainText("NEW-GOAL-VERSION");
    const closeInspector = page.getByRole("button", { name: "Закрыть сведения", exact: true });
    if (await closeInspector.isVisible()) await closeInspector.click();
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 960 });
      await diff.scrollIntoViewIfNeeded();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      ).toBeTruthy();
      expect(
        await diff
          .locator(".diff-values > div")
          .evaluateAll((nodes) =>
            nodes.some((n) => n.scrollWidth > n.clientWidth + 1),
          ),
      ).toBeFalsy();
      await page.screenshot({
        path: `test-results/history-diff-${width}.png`,
        fullPage: true,
      });
    }
    await page.setViewportSize({ width: 1440, height: 960 });
    if (branch) {
      await page
        .getByRole("button", { name: "Исследования", exact: true })
        .click();
      await page.getByRole("tab", { name: /Задания/ }).click();
      const branchRow = page
        .locator(".branch-list .team-row")
        .filter({
          has: page.getByText("Отдельно проверить условия разбиения", {
            exact: true,
          }),
        });
      await branchRow.getByText("Решение по ветви", { exact: true }).click();
      const form = page.getByRole("form", {
        name: `Решение по ветви ${branch.id}`,
      });
      await form
        .getByLabel("Основание решения", { exact: true })
        .fill("Разбить после отдельного утверждения плана");
      await form.getByRole("checkbox").check();
      await form
        .getByRole("button", { name: "Записать решение", exact: true })
        .click();
      await expect(branchRow).toContainText("Приостановлена");
      await branchRow.getByText("Принятые решения: 1", { exact: true }).click();
      await expect(branchRow).toContainText("Ожидается отдельный план");
      state = (await bootstrap()).state;
      expect(state.branches.find((b) => b.id === branch.id).held).toBe(true);
      expect((state.proposals || []).filter((p) => p.study === study.id)).toHaveLength(
        0,
      );
    }
    expect(errors).toEqual([]);
  } finally {
    if (branch)
      await command(`/branches/${branch.id}/decisions`, {
        kind: "stop",
        note: "Завершение браузерного испытания",
        confirm: true,
      });
    if (!wasPaused) await command("/actions", { type: "PAUSE" });
  }
});

test("наблюдение сравнения показывает прогресс без постановок и ключей", async ({
  page,
}) => {
  await page.route("**/api/evaluations", (route) =>
    route.fulfill({
      json: [
        {
          id: "browser-test",
          label: "Учебное сравнение",
          status: "running",
          measured_requests: 3,
          request_upper_bound: 4,
          max_requests: 432,
          summary: {
            terminal_runs: 1,
            expected_runs: 96,
            paired_groups: 0,
            all_recorded: {
              single: {
                runs: 1,
                native_verified: 1,
                reviewed: 0,
                failed: 0,
                incomplete_usage: 0,
              },
            },
          },
          runs: [
            {
              case: "task-002",
              mode: "team",
              repetition: 1,
              status: "running",
            },
          ],
        },
      ],
    }),
  );
  await page.goto("/");
  await page.getByLabel("Ключ доступа").fill(ACCESS_KEY);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page.getByRole("tab", { name: /Задания/ }).click();
  const progress = page.getByRole("region", {
    name: "Выполняющиеся сравнения",
  });
  await expect(progress).toContainText("Завершено запусков: 1 / 96");
  await expect(progress).toContainText("Математическая приемка: не выполнена");
  const closeInspector = page.getByRole("button", { name: "Закрыть сведения", exact: true });
  if (await closeInspector.isVisible()) await closeInspector.click();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 960 });
    await progress.scrollIntoViewIfNeeded();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: `test-results/evaluation-watch-${width}.png`,
      fullPage: true,
    });
  }
});
