import { readFile } from 'node:fs/promises';
import { EPSS_THRESHOLD, evaluateVulnerabilities, resolveCVE } from './epss-policy.mjs';

const reportPaths = process.argv.slice(2);

if (reportPaths.length === 0) {
  console.error('Usage: node scripts/check-epss-threshold.mjs <trivy-report.json>...');
  process.exit(2);
}

function recordVulnerability(vulnerabilities, result, vulnerability, path) {
  const cve = resolveCVE(vulnerability, path);
  const occurrences = vulnerabilities.get(cve) ?? [];
  occurrences.push({
    target: result.Target ?? path,
    package: vulnerability.PkgName ?? 'unknown package',
    version: vulnerability.InstalledVersion ?? 'unknown version',
    severity: vulnerability.Severity ?? 'UNKNOWN',
  });
  vulnerabilities.set(cve, occurrences);
}

async function readReport(path, vulnerabilities) {
  const report = JSON.parse(await readFile(path, 'utf8'));
  for (const result of report.Results ?? []) {
    for (const vulnerability of result.Vulnerabilities ?? []) {
      recordVulnerability(vulnerabilities, result, vulnerability, path);
    }
  }
}

async function readReports(paths) {
  const vulnerabilities = new Map();
  for (const path of paths) await readReport(path, vulnerabilities);
  return vulnerabilities;
}

async function fetchJSON(url) {
  let lastError;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(20000) });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      return await response.json();
    } catch (error) {
      lastError = error;
      if (attempt < 2) await new Promise((resolve) => setTimeout(resolve, 500 * (attempt + 1)));
    }
  }

  throw lastError;
}

async function readEPSS(cves) {
  const scores = new Map();
  for (let start = 0; start < cves.length; start += 60) {
    const params = new URLSearchParams({ cve: cves.slice(start, start + 60).join(','), limit: '1000' });
    const payload = await fetchJSON(`https://api.first.org/data/v1/epss?${params}`);
    for (const item of payload.data ?? []) {
      const score = Number(item.epss);
      if (item.cve && Number.isFinite(score)) scores.set(item.cve, { score, date: item.date });
    }
  }
  return scores;
}

async function readKnownExploitedVulnerabilities() {
  const payload = await fetchJSON(
    'https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json',
  );
  return new Set((payload.vulnerabilities ?? []).map((item) => item.cveID));
}

try {
  const vulnerabilities = await readReports(reportPaths);
  const cves = [...vulnerabilities.keys()].sort();
  const scores = await readEPSS(cves);
  const missingScores = evaluateVulnerabilities(cves, scores, new Set()).missing;
  if (missingScores.length > 0) {
    throw new Error(`EPSS returned no score for ${missingScores.length} CVE(s): ${missingScores.join(', ')}`);
  }

  const knownExploited = await readKnownExploitedVulnerabilities();
  const policy = evaluateVulnerabilities(cves, scores, knownExploited);

  console.log(`Scanned ${cves.length} unique CVEs from ${reportPaths.length} Trivy report(s).`);
  console.log(`EPSS threshold: ${EPSS_THRESHOLD}; highest score: ${policy.highestScore.toFixed(5)}.`);

  for (const cve of policy.aboveThreshold) {
    console.error(`EPSS ${scores.get(cve).score} ${cve}: ${JSON.stringify(vulnerabilities.get(cve))}`);
  }
  for (const cve of policy.knownExploited) {
    console.error(`CISA KEV ${cve}: ${JSON.stringify(vulnerabilities.get(cve))}`);
  }

  if (policy.aboveThreshold.length > 0 || policy.knownExploited.length > 0) {
    process.exitCode = 1;
  } else {
    console.log('No scanned vulnerability exceeds the EPSS threshold or appears in CISA KEV.');
  }
} catch (error) {
  console.error(`Security threshold check failed: ${error.message}`);
  process.exitCode = 1;
}
