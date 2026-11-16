let input = '';

for await (const chunk of process.stdin) input += chunk;

try {
  const report = JSON.parse(input);
  const findings = (report.Results ?? []).flatMap((result) =>
    (result.Secrets ?? []).map((secret) => ({
      target: result.Target ?? 'unknown target',
      rule: secret.RuleID ?? 'unknown rule',
      severity: secret.Severity ?? 'UNKNOWN',
      line: secret.StartLine ?? 0,
    })),
  );

  if (findings.length === 0) {
    console.log('No secrets detected.');
  } else {
    for (const finding of findings) {
      console.error(
        `[${finding.severity}] ${finding.target}:${finding.line} ${finding.rule}`,
      );
    }
    process.exitCode = 1;
  }
} catch {
  console.error('Secret scan returned invalid JSON.');
  process.exitCode = 1;
}
