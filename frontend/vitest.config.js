import { defineConfig } from 'vitest/config';
import { fileURLToPath } from 'url';
import { dirname, resolve } from 'path';

const __dirname = dirname(fileURLToPath(import.meta.url));

/** Creates the Vitest configuration for frontend unit tests. */
export default defineConfig({
  root: __dirname,
  resolve: {
    alias: {
      '@': resolve(__dirname, './public/js'),
    },
  },
  test: {
    environment: 'happy-dom',
    include: ['tests/unit/**/*.test.js'],
    coverage: {
      provider: 'v8',
      include: ['public/js/**/*.js'],
      thresholds: {
        statements: 90,
        branches: 90,
        functions: 90,
        lines: 90,
      },
    },
  }
});
