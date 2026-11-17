import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'line',
  use: {
    baseURL: 'http://127.0.0.1:4174/atterm/',
    channel: process.env.CI ? undefined : 'chrome',
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'npm run docs:build && npm run docs:preview -- --host 127.0.0.1 --port 4174',
    url: 'http://127.0.0.1:4174/atterm/',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
})
