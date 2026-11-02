import { defineConfig, devices } from "@playwright/test";

const databasePath = `/tmp/letterpress-e2e-${process.pid}.db`;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: "list",
  outputDir: "test-results",
  use: {
    baseURL: "http://127.0.0.1:5173",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    ...devices["Desktop Chrome"],
  },
  webServer: [
    {
      command: `cd ../backend && go run ./cmd/server -insecure-cookies -allowed-origin http://127.0.0.1:5173 -fixed-board ABCDEFGHIJKLMNOPQRSTUVWXY -db ${databasePath}`,
      url: "http://127.0.0.1:8080/healthz",
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command: "pnpm dev --host 127.0.0.1",
      url: "http://127.0.0.1:5173",
      reuseExistingServer: false,
      timeout: 120_000,
    },
  ],
});
