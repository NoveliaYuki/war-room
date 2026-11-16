import { spawnSync } from 'node:child_process';

const compose = ['compose', '--project-name', 'war-room-precommit'];
const run = spawnSync('docker', [
  ...compose,
  'up', '--build', '--abort-on-container-exit', '--exit-code-from', 'test', 'test',
], { stdio: 'inherit', env: process.env });
const cleanup = spawnSync('docker', [...compose, 'down'], {
  stdio: 'inherit',
  env: process.env,
});

if (run.error) {
  console.error(`Could not start Docker test stack: ${run.error.message}`);
  process.exitCode = 1;
} else {
  process.exitCode = run.status ?? 1;
}

if (cleanup.error || cleanup.status !== 0) {
  if (cleanup.error) console.error(`Could not clean up Docker test stack: ${cleanup.error.message}`);
  if (process.exitCode === 0) process.exitCode = 1;
}
