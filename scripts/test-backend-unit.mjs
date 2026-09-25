import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const coverageDirectory = mkdtempSync(join(tmpdir(), 'war-room-coverage-'));
const profilePath = join(coverageDirectory, 'unit.out');

try {
  execFileSync(
    'go',
    ['test', '-count=1', '-coverpkg=./pkg/...', `-coverprofile=${profilePath}`, './tests/unit/...'],
    {
      cwd: new URL('../backend/', import.meta.url),
      stdio: 'inherit',
      env: { ...process.env, GOCACHE: process.env.GOCACHE || join(coverageDirectory, 'go-cache') },
    },
  );

  const profile = readFileSync(profilePath, 'utf8').split('\n').slice(1);
  let statements = 0;
  let covered = 0;

  for (const line of profile) {
    const fields = line.trim().split(' ');
    if (fields.length !== 3) continue;
    const count = Number(fields[1]);
    statements += Number(fields[1]);
    if (Number(fields[2]) > 0) covered += count;
  }

  const coverage = statements === 0 ? 100 : (covered / statements) * 100;
  console.log(`Backend unit statement coverage: ${coverage.toFixed(2)}% (required: 90%)`);
  if (coverage < 90) process.exitCode = 1;
} finally {
  rmSync(coverageDirectory, { recursive: true, force: true });
}
