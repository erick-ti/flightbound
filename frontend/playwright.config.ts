import { defineConfig } from "@playwright/test";

// The page tests run against servers that are already up: scripts/smoke.sh
// starts the API on saved data and `next start`, then passes the web origin
// in SMOKE_WEB_ORIGIN.
const baseURL = process.env.SMOKE_WEB_ORIGIN;
if (!baseURL) {
  throw new Error("Set SMOKE_WEB_ORIGIN to the web origin under test, such as http://127.0.0.1:13000.");
}

export default defineConfig({
  testDir: "tests",
  forbidOnly: true,
  retries: 0,
  workers: 1,
  reporter: "list",
  timeout: 30_000,
  expect: { timeout: 10_000 },
  // A failed test leaves a screenshot and a trace in test-results/, which CI
  // uploads.
  use: {
    baseURL,
    browserName: "chromium",
    headless: true,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
});
