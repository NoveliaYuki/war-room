import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { test } from 'node:test';

function checkReport(report) {
  return spawnSync(process.execPath, ['scripts/check-semgrep-report.mjs'], {
    input: report,
    encoding: 'utf8',
  });
}

test('reports an empty Semgrep scan', () => {
  const result = checkReport(JSON.stringify({ results: [] }));

  assert.equal(result.status, 0);
  assert.match(result.stdout, /no security issues/);
});

test('fails with sanitized finding metadata only', () => {
  const secretValue = 'synthetic-secret-value-never-print-this';
  const report = JSON.stringify({
    results: [{
      check_id: 'go.test.security-rule',
      path: 'backend/unsafe.go',
      start: { line: 23 },
      extra: { severity: 'ERROR', lines: secretValue },
    }],
  });
  const result = checkReport(report);
  const output = `${result.stdout}${result.stderr}`;

  assert.equal(result.status, 1);
  assert.match(output, /\[ERROR\] backend\/unsafe\.go:23 go\.test\.security-rule/);
  assert.equal(output.includes(secretValue), false);
});

test('fails closed on malformed or incomplete scanner output', () => {
  for (const report of ['{not-json', '{}']) {
    const result = checkReport(report);

    assert.equal(result.status, 1);
    assert.match(result.stderr, /invalid JSON/);
  }
});
