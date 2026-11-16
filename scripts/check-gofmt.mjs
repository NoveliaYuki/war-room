import { readdir } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, relative, resolve } from 'node:path';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');

/** Collects Go files from the application and Playwright modules. */
async function collectGoFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const nested = await Promise.all(entries.map(async (entry) => {
    const entryPath = resolve(directory, entry.name);
    if (entry.isDirectory()) return collectGoFiles(entryPath);
    return entry.isFile() && entry.name.endsWith('.go') ? [entryPath] : [];
  }));
  return nested.flat();
}

const sourceFiles = (await Promise.all(['backend', 'e2e'].map((directory) =>
  collectGoFiles(resolve(root, directory))
))).flat();
const result = spawnSync('gofmt', ['-l', ...sourceFiles], {
  cwd: root,
  encoding: 'utf8',
  env: process.env,
});

if (result.error) {
  console.error(`Could not start gofmt: ${result.error.message}`);
  process.exitCode = 1;
} else if (result.status !== 0) {
  process.stderr.write(result.stderr);
  process.exitCode = result.status ?? 1;
} else if (result.stdout.trim()) {
  const files = result.stdout.trim().split('\n').map((file) => relative(root, file));
  console.error(`Go files require gofmt:\n${files.join('\n')}`);
  process.exitCode = 1;
} else {
  console.log(`gofmt passed for ${sourceFiles.length} Go files.`);
}
