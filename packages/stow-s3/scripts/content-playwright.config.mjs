import { defineConfig } from "@playwright/test";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../../", import.meta.url));
export default defineConfig({
  testDir: "./browser-tests",
  testMatch: ["content.spec.mjs", "project.spec.mjs", "large-project.spec.mjs"],
  outputDir: `${root}/.cache/content-test-results`,
  timeout: 30_000,
  workers: 1,
  reporter: "line",
  use: { baseURL: "http://127.0.0.1:4181", browserName: "chromium", trace: "retain-on-failure" },
  webServer: {
    command: "node examples/browser-editor/serve.mjs",
    cwd: root,
    url: "http://127.0.0.1:4181/",
    env: { STOW_EDITOR_PORT: "4181", STOW_BROWSER_TESTING: "1" },
    reuseExistingServer: false,
    timeout: 60_000,
    gracefulShutdown: { signal: "SIGTERM", timeout: 30_000 },
  },
});
