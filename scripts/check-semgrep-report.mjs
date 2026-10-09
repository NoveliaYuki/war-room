let input = '';

for await (const chunk of process.stdin) input += chunk;

function safeLabel(value, fallback) {
  const label = typeof value === 'string' ? value : fallback;
  return label.replace(/[\u0000-\u001f\u007f-\u009f]/g, '?').slice(0, 160);
}

try {
  const report = JSON.parse(input);
  if (!Array.isArray(report.results)) throw new Error('invalid report shape');

  if (report.results.length === 0) {
    console.log('Semgrep found no security issues.');
  } else {
    for (const finding of report.results) {
      const severity = safeLabel(finding.extra?.severity, 'UNKNOWN');
      const path = safeLabel(finding.path, 'unknown path');
      const rule = safeLabel(finding.check_id, 'unknown rule');
      const line = Number.isSafeInteger(finding.start?.line) ? finding.start.line : 0;
      console.error(`[${severity}] ${path}:${line} ${rule}`);
    }
    process.exitCode = 1;
  }
} catch {
  console.error('Semgrep returned invalid JSON.');
  process.exitCode = 1;
}
