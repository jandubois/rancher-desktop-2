import * as os from 'os';
import * as path from 'path';

import { defineConfig } from '@playwright/test';

const ci = !!process.env.CI;
const outputDir = path.join(import.meta.dirname, '..', 'e2e', 'test-results');
const testDir = path.join(import.meta.dirname, '..', '..', 'e2e');
// The provisioned github runners are much slower overall than cirrus's, so allow 2 hours for a full e2e run
const timeScale = ci ? 4 : 1;

const config = defineConfig({
  testDir,
  outputDir,
  forbidOnly:    ci,
  timeout:       10 * 60 * 1000 * timeScale,
  globalTimeout: 30 * 60 * 1000 * timeScale,
  workers:       ci ? 1 : os.availableParallelism(),
  reporter:      ci ? [['github'], ['list']] : 'list',
  retries:       ci ? 2 : 0,
  use:           {
    trace: {
      mode:        'on-all-retries',
      screenshots: true,
    },
  },
});

// Playwright's default stack trace hook does not understand that we're using tsx
// to run the tests; remove it to use NodeJS's default source map support.
delete (Error as any).prepareStackTrace;

export default config;
