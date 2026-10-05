import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath } from "node:url";
import { execFileSync, spawnSync } from "node:child_process";

const source = process.argv[2];
if (!/^[a-z][a-z0-9_]*_test$/.test(source || "")) {
  throw new Error("Pass a stopped test database name ending in _test");
}
const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const target = "research_restore_" + crypto.randomBytes(6).toString("hex") + "_test";
const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "research-pg-backup-"));
const file = path.join(temporary, "database.dump");
const prefix = ["compose", "--env-file", ".env.postgres", "exec", "-T", "postgres"];
const command = (...args) => execFileSync("docker", [...prefix, ...args], {
  cwd: repo, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"],
});
const sql = (database, query) => command("psql", "-U", "postgres", "-d", database, "-At", "-v", "ON_ERROR_STOP=1", "-c", query);
let created = false;
try {
  const tables = sql(source, "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename").trim().split("\n");
  if (!tables.includes("schema_migrations")) throw new Error("Source has no research schema");
  const query = tables.map((name, i) =>
    "SELECT " + i + " AS n,count(*),md5(COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text)::text,chr(48))) FROM \"" + name.replaceAll('"', '""') + "\" r"
  ).join(" UNION ALL ") + " ORDER BY n";
  const expected = sql(source, query);
  const out = fs.openSync(file, "wx", 0o600);
  try {
    const r = spawnSync("docker", [...prefix, "pg_dump", "-U", "postgres", "-d", source, "-Fc"], {cwd: repo, stdio: ["ignore", out, "pipe"]});
    if (r.status !== 0) throw new Error("pg_dump failed");
  } finally { fs.closeSync(out); }
  command("createdb", "-U", "postgres", "-O", "research", target);
  created = true;
  const input = fs.openSync(file, "r");
  try {
    const r = spawnSync("docker", [...prefix, "pg_restore", "-U", "postgres", "--role=research", "--no-owner", "--exit-on-error", "-d", target], {cwd: repo, stdio: [input, "ignore", "pipe"]});
    if (r.status !== 0) throw new Error("pg_restore failed");
  } finally { fs.closeSync(input); }
  if (sql(target, query) !== expected || sql(source, query) !== expected) {
    throw new Error("Restored contents differ or source changed during verification");
  }
  console.log(JSON.stringify({tablesCompared: tables.length, restoredDataMatch: true}));
} finally {
  if (created) command("dropdb", "-U", "postgres", target);
  fs.rmSync(temporary, {recursive: true, force: true});
}
