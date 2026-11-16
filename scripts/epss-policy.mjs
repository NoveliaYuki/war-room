/** Maximum allowed EPSS probability for vulnerabilities in scanned components. */
export const EPSS_THRESHOLD = 0.05;

/** Resolves a Trivy vulnerability identifier to its CVE alias. */
export function resolveCVE(vulnerability, reportPath) {
  const aliases = [vulnerability.VulnerabilityID, ...(vulnerability.Aliases ?? [])];
  const cve = aliases.find((id) => /^CVE-\d{4}-\d+$/.test(id));
  if (!cve) throw new Error(`Cannot map ${vulnerability.VulnerabilityID} to a CVE in ${reportPath}`);
  return cve;
}

/** Applies the EPSS threshold, score availability, and CISA KEV policy. */
export function evaluateVulnerabilities(cves, scores, knownExploited, threshold = EPSS_THRESHOLD) {
  return {
    missing: cves.filter((cve) => !scores.has(cve)),
    aboveThreshold: cves.filter((cve) => scores.get(cve)?.score > threshold),
    knownExploited: cves.filter((cve) => knownExploited.has(cve)),
    highestScore: Math.max(0, ...cves.map((cve) => scores.get(cve)?.score ?? 0)),
  };
}
