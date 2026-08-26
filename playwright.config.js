const { defineConfig, devices } = require('@playwright/test');

const baseURL = 'http://127.0.0.1:18181';

module.exports = defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  reporter: 'html',
  webServer: {
    command: './scripts/run-e2e-server.sh',
    url: baseURL,
    reuseExistingServer: false,
    timeout: 120_000,
  },
  use: {
    baseURL,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'firefox',
      use: { ...devices['Desktop Firefox'] },
    },
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
