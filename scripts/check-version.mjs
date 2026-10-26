import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { getReleaseVersion, parseVersion, root, validateBump } from './version-utils.mjs';

function verifySingleVersionSource() {
  const manifest = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'));
  if ('version' in manifest) {
    throw new Error('Remove package.json version; VERSION is the sole application version source');
  }

  for (const dockerfile of ['backend/Dockerfile', 'frontend/Dockerfile']) {
    const content = readFileSync(resolve(root, dockerfile), 'utf8');
    if (/\bversion\s*=/i.test(content)) {
      throw new Error(`Remove the application version label from ${dockerfile}; VERSION is the sole version source`);
    }
  }
}

try {
  const version = getReleaseVersion();
  parseVersion(version, 'VERSION');
  verifySingleVersionSource();
  const baseVersion = process.env.BASE_VERSION;
  if (baseVersion) {
    const bump = process.env.VERSION_BUMP || 'patch';
    validateBump(baseVersion, version, bump);
    console.log(`VERSION ${version} is the only app version and the exact ${bump} bump from ${baseVersion}.`);
  } else {
    console.log(`VERSION ${version} is the only app version.`);
  }
} catch (error) {
  console.error(`Version validation failed: ${error.message}`);
  process.exitCode = 1;
}
