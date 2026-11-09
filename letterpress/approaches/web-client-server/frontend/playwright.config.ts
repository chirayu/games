import { defineConfig, devices } from "@playwright/test";

const databasePath = `/tmp/letterpress-e2e-${process.pid}.db`;
const backendPort = process.env.LETTERPRESS_E2E_BACKEND_PORT ?? "8080";
const frontendPort = process.env.LETTERPRESS_E2E_FRONTEND_PORT ?? "5173";
const backendURL = `http://127.0.0.1:${backendPort}`;
const frontendURL = `http://127.0.0.1:${frontendPort}`;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: "list",
  outputDir: "test-results",
  use: {
    baseURL: frontendURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    ...devices["Desktop Chrome"],
  },
  webServer: [
    {
      command: `cd ../backend && GOCACHE=/tmp/letterpress-e2e-go-cache go run ./cmd/server -addr 127.0.0.1:${backendPort} -insecure-cookies -allowed-origin ${frontendURL} -fixed-board CATDOGSUNMAPEXTRAORDINARY -db ${databasePath}`,
      url: `${backendURL}/healthz`,
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command: `cd ../backend && until curl -fsS ${backendURL}/healthz >/dev/null; do sleep 0.2; done; GOCACHE=/tmp/letterpress-e2e-go-cache LETTERPRESS_BOT_TOKEN=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA go run ./cmd/bot-runtime -db ${databasePath} -api-base-url ${backendURL} -poll-interval 200ms`,
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command: `LETTERPRESS_API_TARGET=${backendURL} pnpm dev --host 127.0.0.1 --port ${frontendPort}`,
      url: frontendURL,
      reuseExistingServer: false,
      timeout: 120_000,
    },
  ],
});
