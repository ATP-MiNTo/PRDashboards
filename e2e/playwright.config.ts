import { existsSync } from "node:fs";
import { defineConfig } from "@playwright/test";

// The app under test is the e2e build with in-process fake GitHub and Atlassian servers.
// Fixed ports: app 4317, fake GitHub 4411, fake Atlassian 4412.
// PW_CHROMIUM_PATH wins, then the preinstalled browser if present, else Playwright's own download.
const preinstalled = "/opt/pw-browsers/chromium";
const executablePath =
  process.env.PW_CHROMIUM_PATH ||
  (existsSync(preinstalled) ? preinstalled : undefined);

export default defineConfig({
  testDir: ".",
  testMatch: "*.spec.ts",
  workers: 1,
  fullyParallel: false,
  retries: 0,
  reporter: [["list"]],
  timeout: 30_000,
  use: {
    baseURL: "http://127.0.0.1:4317",
    trace: "off",
    launchOptions: { executablePath },
  },
  webServer: {
    command:
      "bin/prdash-e2e --fake --no-browser --port 4317 --data-dir .e2e-data",
    cwd: "..",
    url: "http://127.0.0.1:4317/auth/launch/",
    reuseExistingServer: false,
    timeout: 30_000,
  },
});
