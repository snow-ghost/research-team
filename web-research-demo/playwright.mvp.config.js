import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests/mvp",
  timeout: 45000,
  workers: 1,
  retries: 0,
  webServer: {
    command: "node tests/fixtures/mvp-server.mjs",
    url: "http://127.0.0.1:4190",
    reuseExistingServer: false,
    timeout: 180000,
  },
  use: {
    baseURL: "http://127.0.0.1:4190",
    headless: true,
    launchOptions: { executablePath: process.env.CHROME_PATH || undefined },
  },
  reporter: "list",
});
