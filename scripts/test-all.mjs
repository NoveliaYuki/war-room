import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm';
const suites = [
  {
    name: 'Single version source and release bump validation',
    command: process.execPath,
    args: ['scripts/check-version.mjs'],
    cwd: root,
  },
  {
    name: 'Frontend dependency vulnerability audit (high severity)',
    command: npm,
    args: ['audit', '--prefix', 'frontend', '--audit-level=high'],
    cwd: root,
  },
  {
    name: 'Backend and frontend quality gates',
    command: npm,
    args: ['run', 'lint'],
    cwd: root,
  },
  {
    name: 'Backend unit tests (95% statement coverage)',
    command: process.execPath,
    args: ['scripts/test-backend-unit.mjs'],
    cwd: root,
  },
  {
    name: 'Frontend unit tests (95% statements, lines, and functions; 90% branches)',
    command: npm,
    args: ['run', 'test:unit'],
    cwd: resolve(root, 'frontend'),
  },
  {
    name: 'Backend integration tests',
    command: 'go',
    args: ['test', '-count=1', './tests/integration/...'],
    cwd: resolve(root, 'backend'),
  },
  {
    name: 'Playwright browser tests',
    command: 'go',
    args: ['test', '-count=1', './tests/...'],
    cwd: resolve(root, 'e2e'),
  },
];

let failures = 0;
for (const suite of suites) {
  console.log(`\n=== ${suite.name} ===`);
  const result = spawnSync(suite.command, suite.args, {
    cwd: suite.cwd,
    stdio: 'inherit',
    env: process.env,
  });
  if (result.error || result.status !== 0) failures += 1;
}

process.exitCode = failures === 0 ? 0 : 1;
