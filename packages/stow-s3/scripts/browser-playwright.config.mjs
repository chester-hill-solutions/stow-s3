import { defineConfig } from "@playwright/test";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const live = process.env.STOW_BROWSER_ALLOW_LIVE === "1";
export default defineConfig({
  testDir: "./browser-tests",
  outputDir: `${root}/.cache/browser-test-results`,
  timeout: live ? 120_000 : 30_000,
  expect: { timeout: 10_000 },
  workers: 1,
  retries: 0,
  forbidOnly: Boolean(process.env.CI),
  reporter: [["line"], ["json", { outputFile: `${root}/.cache/browser-test-results/report.json` }]],
  use: { baseURL: "http://127.0.0.1:4180", browserName: "chromium", trace: live ? "off" : "retain-on-failure", screenshot: "only-on-failure" },
  webServer: {
    command: "node examples/browser-editor/serve.mjs",
    cwd: root,
    url: "http://127.0.0.1:4180/",
    env: { STOW_EDITOR_PORT: "4180", STOW_BROWSER_TESTING: "1" },
    reuseExistingServer: false,
    timeout: 60_000,
    gracefulShutdown: { signal: "SIGTERM", timeout: 30_000 },
  },
});
