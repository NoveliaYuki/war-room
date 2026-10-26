import assert from 'node:assert/strict';
import test from 'node:test';
import { EPSS_THRESHOLD, evaluateVulnerabilities, resolveCVE } from './epss-policy.mjs';

test('allows a score equal to the EPSS threshold', () => {
  const scores = new Map([['CVE-2026-1234', { score: EPSS_THRESHOLD }]]);
  const result = evaluateVulnerabilities(['CVE-2026-1234'], scores, new Set());

  assert.deepEqual(result.aboveThreshold, []);
  assert.equal(result.highestScore, EPSS_THRESHOLD);
});

test('rejects vulnerabilities above the EPSS threshold', () => {
  const scores = new Map([['CVE-2026-1234', { score: EPSS_THRESHOLD + 0.001 }]]);
  const result = evaluateVulnerabilities(['CVE-2026-1234'], scores, new Set());

  assert.deepEqual(result.aboveThreshold, ['CVE-2026-1234']);
});

test('fails closed when EPSS has no score', () => {
  const result = evaluateVulnerabilities(['CVE-2026-1234'], new Map(), new Set());

  assert.deepEqual(result.missing, ['CVE-2026-1234']);
});

test('rejects known exploited vulnerabilities even below the EPSS threshold', () => {
  const cve = 'CVE-2026-1234';
  const result = evaluateVulnerabilities(
    [cve],
    new Map([[cve, { score: 0.001 }]]),
    new Set([cve]),
  );

  assert.deepEqual(result.knownExploited, [cve]);
  assert.deepEqual(result.aboveThreshold, []);
});

test('resolves CVE identifiers from Trivy aliases', () => {
  assert.equal(resolveCVE({ VulnerabilityID: 'GHSA-test', Aliases: ['CVE-2026-1234'] }, 'report'), 'CVE-2026-1234');
});

test('rejects vulnerabilities that cannot be mapped to a CVE', () => {
  assert.throws(() => resolveCVE({ VulnerabilityID: 'GHSA-test' }, 'report'), /Cannot map GHSA-test/);
});
