import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { getBaseVersion, getReleaseVersion, parseVersion, root, validateBump } from './version-utils.mjs';

function verifySynchronization(version) {
  const manifest = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'));
  if (manifest.version !== version) {
    throw new Error(`package.json version ${manifest.version} does not match VERSION ${version}`);
  }

  const readme = readFileSync(resolve(root, 'README.md'), 'utf8');
  if (!readme.startsWith(`# War Room ${version}\n`)) {
    throw new Error(`README.md release heading does not match VERSION ${version}`);
  }

  for (const dockerfile of ['backend/Dockerfile', 'frontend/Dockerfile']) {
    const content = readFileSync(resolve(root, dockerfile), 'utf8');
    const label = /version="([^"]+)"/i.exec(content)?.[1];
    if (label !== version) {
      throw new Error(`${dockerfile} version label ${label ?? '(missing)'} does not match VERSION ${version}`);
    }
  }
}

try {
  const version = getReleaseVersion();
  parseVersion(version, 'VERSION');
  verifySynchronization(version);
  const baseVersion = process.env.BASE_VERSION || getBaseVersion();
  const bump = process.env.VERSION_BUMP || 'patch';
  validateBump(baseVersion, version, bump);
  console.log(`Version ${version} is synchronized and is the exact ${bump} bump from ${baseVersion}.`);
} catch (error) {
  console.error(`Version validation failed: ${error.message}`);
  process.exitCode = 1;
}
