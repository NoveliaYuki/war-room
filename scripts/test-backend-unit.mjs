import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const coverageDirectory = mkdtempSync(join(tmpdir(), 'war-room-coverage-'));
const profilePaths = {
  unit: join(coverageDirectory, 'unit.out'),
  config: join(coverageDirectory, 'config.out'),
  database: join(coverageDirectory, 'database.out'),
  handlers: join(coverageDirectory, 'handlers.out'),
  models: join(coverageDirectory, 'models.out'),
  middleware: join(coverageDirectory, 'middleware.out'),
  repository: join(coverageDirectory, 'repository.out'),
  service: join(coverageDirectory, 'service.out'),
};

try {
  const commandEnv = {
    cwd: new URL('../backend/', import.meta.url),
    stdio: 'inherit',
    env: { ...process.env, GOCACHE: process.env.GOCACHE || join(coverageDirectory, 'go-cache') },
  };
  for (const [suite, packagePath] of [
    ['unit', './tests/unit/...'],
    ['config', './pkg/config/...'],
    ['database', './pkg/database/...'],
    ['handlers', './pkg/handlers/...'],
    ['models', './pkg/models/...'],
    ['middleware', './pkg/middleware/...'],
    ['repository', './pkg/repository/...'],
    ['service', './pkg/service/...'],
  ]) {
    execFileSync('go', [
      'test', '-count=1', '-coverpkg=./pkg/...', `-coverprofile=${profilePaths[suite]}`, packagePath,
    ], commandEnv);
  }

  const blocks = new Map();
  for (const profilePath of Object.values(profilePaths)) {
    const profile = readFileSync(profilePath, 'utf8').split('\n').slice(1);
    for (const line of profile) {
      const fields = line.trim().split(' ');
      if (fields.length !== 3) continue;
      const key = `${fields[0]} ${fields[1]}`;
      const block = blocks.get(key) || { statements: Number(fields[1]), executions: 0 };
      block.executions += Number(fields[2]);
      blocks.set(key, block);
    }
  }

  let statements = 0;
  let covered = 0;

  for (const block of blocks.values()) {
    statements += block.statements;
    if (block.executions > 0) covered += block.statements;
  }

  const coverage = statements === 0 ? 100 : (covered / statements) * 100;
  console.log(`Backend unit statement coverage: ${coverage.toFixed(2)}% (required: 95%)`);
  if (coverage < 95) {
    const uncovered = [...blocks.entries()]
      .filter(([, block]) => block.executions === 0)
      .map(([key]) => key)
      .sort()
      .slice(0, 30);
    console.log(`First uncovered backend blocks:\n${uncovered.join('\n')}`);
    process.exitCode = 1;
  }
} finally {
  rmSync(coverageDirectory, { recursive: true, force: true });
}
