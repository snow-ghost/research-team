import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests/browser",
  timeout: 30000,
  workers: 1,
  retries: 0,
  webServer: process.env.DEMO_URL
    ? undefined
    : {
        command: "npm run dev -- --port 4187 --strictPort",
        url: "http://127.0.0.1:4187",
        reuseExistingServer: !process.env.CI,
        timeout: 60000,
      },
  use: {
    baseURL: process.env.DEMO_URL || "http://127.0.0.1:4187",
    headless: true,
    launchOptions: {
      executablePath: process.env.CHROME_PATH || undefined,
    },
  },
  reporter: "list",
});
