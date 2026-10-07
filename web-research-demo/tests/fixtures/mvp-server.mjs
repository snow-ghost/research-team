import http from "node:http";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync, spawn } from "node:child_process";
import { ACCESS_KEY, SOURCE_COMMIT } from "./constants.js";

const repo = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../..",
);
const databaseURL = process.env.MVP_DATABASE_URL;
if (databaseURL && !new URL(databaseURL).pathname.endsWith("_test")) {
  throw new Error(
    "MVP_DATABASE_URL must name a separate database ending in _test",
  );
}
const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "research-web-test-"));
const repository = "test-owner/lab";
const bot = "test-bot",
  owner = "test-owner";
let issue = null,
  pull = null,
  issueComments = [],
  pullComments = [],
  reviews = [],
  inline = [];
const send = (res, value, status = 200) => {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(value));
};
const fake = http.createServer(async (req, res) => {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  const text = Buffer.concat(chunks).toString();
  const body = text ? JSON.parse(text) : {};
  const url = new URL(req.url, "http://localhost");
  if (url.pathname === "/v1/chat/completions") {
    if (text.includes("WAIT_FOR_CANCEL")) return;
    send(res, {
      choices: [
        {
          message: {
            role: "assistant",
            content: text.includes("Предложи план исследования.")
              ? JSON.stringify({
                  summary: "Сначала лемма, затем покрытие и исходная цель.",
                  claims: [
                    {
                      title: "Независимая лемма плана",
                      statement: "True",
                      assumptions: "Prop",
                      formal_goal: {
                        source: "def Statement : Prop := True",
                        declaration: "Statement",
                        candidate: "Candidate",
                      },
                    },
                  ],
                  coverage: {
                    title: "Покрытие плана",
                    statement: "Из леммы следует точная исходная цель.",
                    assumptions: "Принятая лемма",
                    formal_goal: {
                      source: "def Statement : Prop := True",
                      declaration: "Statement",
                      candidate: "Candidate",
                    },
                  },
                  strategies: [
                    {
                      target: 0,
                      method: "induction",
                      priority: 20,
                      rationale: "Начать с независимого основания.",
                    },
                    {
                      target: 1,
                      method: "decomposition",
                      priority: 90,
                      rationale: "Покрытие после приемки леммы.",
                    },
                    {
                      target: -1,
                      method: "equivalence",
                      priority: 100,
                      rationale: "Завершить точную исходную цель.",
                    },
                  ],
                })
              : text.includes("ROLE_counterexample")
                ? JSON.stringify({
                    outcome: "none_found",
                    evidence: "Проверены конечные граничные случаи.",
                  })
                : text.includes("ROLE_review")
                  ? JSON.stringify({
                      summary: "Проверено на учебном примере.",
                      findings: [],
                    })
                  : "Кандидат доказательства. Предпосылки проверены на учебном примере.",
          },
          finish_reason: "stop",
        },
      ],
      usage: { prompt_tokens: 20, completion_tokens: 12 },
    });
    return;
  }
  if (url.pathname === "/github/user") return send(res, { login: owner });
  const route = url.pathname.replace("/github/repos/" + repository, "");
  if (route === "") return send(res, { full_name: repository, private: true });
  if (route === "/git/ref/heads/main")
    return send(res, { object: { sha: SOURCE_COMMIT } });
  if (route === "/issues" && req.method === "POST") {
    issue = {
      number: 1,
      title: body.title,
      body: body.body,
      state: "open",
      user: { login: owner },
      assignees: [{ login: bot }],
      updated_at: "2026-09-20T00:00:00Z",
    };
    issueComments = [
      {
        id: 101,
        body: "План: добавить проверку граничных случаев.\nDoes this approach work for you? Reply with **yes**",
        user: { login: bot },
        updated_at: "2026-09-20T00:00:01Z",
      },
    ];
    return send(res, issue, 201);
  }
  if (route === "/issues") return send(res, issue ? [issue] : []);
  if (route === "/issues/1") return send(res, issue);
  if (route === "/issues/1/comments" || route === "/issues/2/comments") {
    const target = route.includes("/1/") ? issueComments : pullComments;
    if (req.method === "POST") {
      const comment = {
        id: 102 + target.length,
        body: body.body,
        user: { login: owner },
        updated_at: "2026-09-20T00:00:02Z",
      };
      target.push(comment);
      if (body.body.startsWith("yes\n"))
        pull = {
          number: 2,
          title: "Проверка граничных случаев",
          body: "Closes #1",
          state: "open",
          draft: false,
          merged: false,
          user: { login: bot },
          changed_files: 1,
          head: {
            ref: "1-" + issue.title,
            sha: "2".repeat(40),
            repo: { full_name: repository },
          },
          base: {
            ref: "main",
            sha: SOURCE_COMMIT,
            repo: { full_name: repository },
          },
          updated_at: "2026-09-20T00:00:03Z",
        };
      return send(res, comment, 201);
    }
    return send(res, target);
  }
  if (route === "/pulls") return send(res, pull ? [pull] : []);
  if (route === "/pulls/2") return send(res, pull);
  if (route === "/pulls/2/comments") return send(res, inline);
  if (route === "/pulls/2/reviews") {
    if (req.method === "POST") {
      const review = {
        id: 201,
        body: body.body,
        state: "CHANGES_REQUESTED",
        commit_id: body.commit_id,
        user: { login: owner },
      };
      reviews.push(review);
      inline = body.comments.map((c, i) => ({
        id: 301 + i,
        body: c.body,
        path: c.path,
        line: c.line,
        commit_id: body.commit_id,
        pull_request_review_id: 201,
        user: { login: owner },
      }));
      return send(res, review, 201);
    }
    return send(res, reviews);
  }
  if (route === "/pulls/2/files")
    return send(res, [
      {
        sha: "3".repeat(40),
        filename: "checker.go",
        status: "added",
        patch: "@@ -0,0 +1 @@\n+package checker",
      },
    ]);
  console.error("Unexpected fixture route", req.method, url.pathname);
  send(res, { error: "unknown fixture route" }, 404);
});
await new Promise((resolve) => fake.listen(0, "127.0.0.1", resolve));
const base = "http://127.0.0.1:" + fake.address().port;
const write = (name, value) => {
  const file = path.join(temporary, name);
  fs.writeFileSync(file, JSON.stringify(value), { mode: 0o600 });
  return file;
};
const profile = write("profile.json", {
  id: "fixture-reader",
  kind: "model",
  skills: [
    { id: "proof-audit", version: "1", instructions: "Check all assumptions." },
  ],
  limits: {
    timeout_seconds: 15,
    max_steps: 2,
    max_tool_calls: 0,
    max_output_tokens: 512,
    max_output_bytes: 1048576,
  },
  model: {
    protocol: "chat_completions",
    base_url: base + "/v1",
    model: "fixture-model",
    token_env: "MODEL_TOKEN",
    allow_loopback_http: true,
  },
});
const coddy = write("coddy.json", {
  api_url: base + "/github",
  api_version: "2026-03-10",
  repository,
  bot_login: bot,
  requester_login: owner,
  base_branch: "main",
  token_env: "GITHUB_TOKEN",
  coddy_revision: "fc3b9491d76d5911aa78e51fc6494ddf3c21bb77",
  coddy_patchset: "review-loop-v1",
  timeout_seconds: 5,
  max_pages: 3,
  allow_loopback_http: true,
});
const roleProfiles = ["proof", "counterexample", "review"].map((role) => {
  const value = JSON.parse(fs.readFileSync(profile, "utf8"));
  value.id = "fixture-" + role;
  value.skills = [{ id: role, version: "1", instructions: "ROLE_" + role }];
  return {
    file: write("profile-" + role + ".json", value),
    label: "Исполнитель: " + role,
  };
});
const config = write("server.json", {
  listen: "127.0.0.1:4190",
  data_dir: path.join(temporary, "state"),
  web_dir: path.join(repo, "web-research-demo/dist"),
  token_env: "RESEARCH_WEB_TOKEN",
  max_parallel: 1,
  ...(databaseURL
    ? {
        database: { driver: "postgres", dsn_env: "RESEARCH_TEST_DATABASE_URL" },
      }
    : {}),
  profiles: [
    { file: profile, label: "Проверочный исполнитель" },
    ...roleProfiles,
  ],
  workspaces: [
    {
      id: "example",
      label: "Учебные материалы",
      path: path.join(repo, "examples/agents/workspace"),
    },
  ],
  coddy_config: coddy,
  ...(process.env.MVP_LEAN_CONFIG
    ? { lean_config: process.env.MVP_LEAN_CONFIG }
    : {}),
});
const binary = path.join(temporary, "research-server");
execFileSync("go", ["build", "-o", binary, "./cmd/research-server"], {
  cwd: repo,
  stdio: "inherit",
});
const child = spawn(binary, ["-config", config], {
  cwd: repo,
  stdio: "inherit",
  env: {
    PATH: process.env.PATH,
    HOME: temporary,
    RESEARCH_WEB_TOKEN: ACCESS_KEY,
    MODEL_TOKEN: "test-model-key",
    GITHUB_TOKEN: "test-github-key",
    ...(databaseURL ? { RESEARCH_TEST_DATABASE_URL: databaseURL } : {}),
  },
});
let stopping = false;
function stop() {
  if (stopping) return;
  stopping = true;
  child.kill("SIGTERM");
  fake.closeAllConnections();
  fake.close();
}
process.on("SIGINT", stop);
process.on("SIGTERM", stop);
child.on("exit", (code) => {
  stop();
  fs.rmSync(temporary, { recursive: true, force: true });
  process.exit(code || 0);
});
