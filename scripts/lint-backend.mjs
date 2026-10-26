import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const config = resolve(root, 'backend/.golangci.yml');
const modules = ['backend', 'e2e'];

for (const module of modules) {
  console.log(`Linting Go module: ${module}`);
  const result = spawnSync('golangci-lint', ['run', '--config', config, './...'], {
    cwd: resolve(root, module),
    stdio: 'inherit',
    env: process.env,
  });

  if (result.error) {
    console.error(`Could not start golangci-lint for ${module}: ${result.error.message}`);
    process.exitCode = 1;
  } else if (result.status !== 0) {
    process.exitCode = result.status ?? 1;
  }
}
