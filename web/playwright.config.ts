import { defineConfig } from "@playwright/test";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
const dataDir =
  process.env.NAV_E2E_DATA_DIR || mkdtempSync(join(tmpdir(), "nav-tool-e2e-"));
process.env.NAV_E2E_DATA_DIR = dataDir;
export default defineConfig({
  testDir: "./tests",
  workers: 1,
  fullyParallel: false,
  timeout: 60000,
  use: {
    baseURL: "http://127.0.0.1:31516",
    viewport: { width: 1440, height: 960 },
    trace: "retain-on-failure",
  },
  webServer: {
    command: `../bin/navtool -addr 127.0.0.1:31516 -data ${dataDir} -web dist`,
    url: "http://127.0.0.1:31516/api/auth/status",
    reuseExistingServer: false,
    timeout: 30000,
  },
  reporter: "list",
});
