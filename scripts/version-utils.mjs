import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

/** Resolves the repository root from the release helper's location. */
export const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const firstReleaseVersion = '0.0.1';
const initialBaselineVersion = '0.0.0';

/** Read and validate a strict three-part semantic version. */
export function parseVersion(value, source) {
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.exec(value);
  if (!match) throw new Error(`${source} must contain a stable semantic version (for example 0.0.1); got ${JSON.stringify(value)}`);
  return match.slice(1).map(Number);
}

/** Read the release version from VERSION. */
export function getReleaseVersion() {
  return readFileSync(resolve(root, 'VERSION'), 'utf8').trim();
}

/** Find the most recent committed package version different from the working release. */
export function getBaseVersion() {
  if (getReleaseVersion() === firstReleaseVersion) return initialBaselineVersion;

  const commits = execFileSync('git', ['log', '--format=%H', '--', 'package.json'], {
    cwd: root,
    encoding: 'utf8',
  }).trim().split('\n').filter(Boolean);

  for (const commit of commits) {
    try {
      const manifest = execFileSync('git', ['show', `${commit}:package.json`], {
        cwd: root,
        encoding: 'utf8',
      });
      const version = JSON.parse(manifest).version;
      parseVersion(version, `${commit}:package.json version`);
      if (version !== getReleaseVersion()) return version;
    } catch (error) {
      if (error instanceof SyntaxError) throw error;
    }
  }
  throw new Error('Could not find a prior committed package version; set BASE_VERSION explicitly.');
}

/** Verify that target version is exactly the requested semantic bump from base. */
export function validateBump(baseVersion, targetVersion, bump) {
  const base = parseVersion(baseVersion, 'BASE_VERSION');
  const target = parseVersion(targetVersion, 'VERSION');
  if (!['patch', 'minor', 'major'].includes(bump)) {
    throw new Error(`VERSION_BUMP must be patch, minor, or major; got ${JSON.stringify(bump)}`);
  }

  const expected = [...base];
  if (bump === 'major') {
    expected[0] += 1;
    expected[1] = 0;
    expected[2] = 0;
  } else if (bump === 'minor') {
    expected[1] += 1;
    expected[2] = 0;
  } else {
    expected[2] += 1;
  }

  const expectedVersion = expected.join('.');
  if (targetVersion !== expectedVersion) {
    throw new Error(`VERSION ${targetVersion} is not the exact ${bump} bump from ${baseVersion}; expected ${expectedVersion}`);
  }
}
