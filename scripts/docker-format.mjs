import { randomUUID } from 'node:crypto';
import { readdir } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, relative, resolve } from 'node:path';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const projectName = `war-room-format-${process.pid}-${randomUUID().slice(0, 8)}`;

/** Collects source files that the container formatter should update. */
async function collectGoFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const nested = await Promise.all(entries.map(async (entry) => {
    const entryPath = resolve(directory, entry.name);
    if (entry.isDirectory()) return collectGoFiles(entryPath);
    return entry.isFile() && entry.name.endsWith('.go') ? [entryPath] : [];
  }));
  return nested.flat();
}

const files = (await Promise.all(['backend', 'e2e'].map((directory) =>
  collectGoFiles(resolve(root, directory))
))).flat().map((file) => relative(root, file));
const compose = ['compose', '--profile', 'test', '--project-name', projectName];
const format = spawnSync('docker', [
  ...compose,
  'run', '--build', '--rm', '--no-deps',
  '--volume', `${root}:/workspace`, '--workdir', '/workspace',
  'test', 'gofmt', '-w', ...files,
], { cwd: root, stdio: 'inherit', env: process.env });
const cleanup = spawnSync('docker', [...compose, 'down', '-v'], {
  cwd: root,
  stdio: 'inherit',
  env: process.env,
});

if (format.error) {
  console.error(`Could not start Docker-based Go formatter: ${format.error.message}`);
  process.exitCode = 1;
} else if (format.status !== 0) {
  process.exitCode = format.status ?? 1;
}
if (cleanup.error || cleanup.status !== 0) {
  if (cleanup.error) console.error(`Could not clean up formatter project: ${cleanup.error.message}`);
  process.exitCode = 1;
}
