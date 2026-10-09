import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { test } from 'node:test';

function checkReport(report) {
  return spawnSync(process.execPath, ['scripts/check-trivy-secrets.mjs'], {
    input: report,
    encoding: 'utf8',
  });
}

test('reports a clean secret scan', () => {
  const result = checkReport(JSON.stringify({ Results: [{ Secrets: [] }] }));

  assert.equal(result.status, 0);
  assert.match(result.stdout, /No secrets detected/);
});

test('fails with sanitized finding metadata', () => {
  const secretValue = 'synthetic-secret-value-never-print-this';
  const report = JSON.stringify({
    Results: [{
      Target: 'src/config.js',
      Secrets: [{
        RuleID: 'test-rule',
        Severity: 'HIGH',
        StartLine: 17,
        Match: secretValue,
      }],
    }],
  });
  const result = checkReport(report);
  const output = `${result.stdout}${result.stderr}`;

  assert.equal(result.status, 1);
  assert.match(output, /\[HIGH\] src\/config\.js:17 test-rule/);
  assert.equal(output.includes(secretValue), false);
});

test('fails closed on malformed scanner output', () => {
  const result = checkReport('{not-json');

  assert.equal(result.status, 1);
  assert.match(result.stderr, /invalid JSON/);
});
