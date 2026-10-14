// Run on the Railway verification runner, never on the local workstation.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdir, writeFile, readFile } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { join } from 'node:path';

const root = '/results/release-030-published';
const expectedCommit = '09421edbe3d66e4653795d5459836547307d57c7';
const expectedNames = [
  'SHA256SUMS.txt', 'stow-darwin-amd64.tar.gz',
  'stow-darwin-arm64.tar.gz', 'stow-linux-amd64.tar.gz',
  'stow-linux-arm64.tar.gz',
].sort();
const sha256 = bytes => createHash('sha256').update(bytes).digest('hex');

async function download(url) {
  for (let attempt = 0; attempt < 3; attempt++) {
    const response = await fetch(url, {
      headers: { 'User-Agent': 'stow-release-verification', Accept: '*/*' },
      signal: AbortSignal.timeout(30000),
    });
    if (response.ok) return Buffer.from(await response.arrayBuffer());
    if (attempt === 2 || (response.status !== 429 && response.status < 500)) {
      throw new Error(`Download failed (${response.status}): ${url}`);
    }
    await new Promise(resolve => setTimeout(resolve, 1000 * (attempt + 1)));
  }
}

await mkdir(root, { recursive: true });
const release = JSON.parse(await download('https://api.github.com/repos/chester-hill-solutions/stow-s3/releases/tags/v0.3.0'));
assert.equal(release.tag_name, 'v0.3.0');
assert.equal(release.draft, false);
assert.equal(release.prerelease, false);
assert.ok(release.published_at);
assert.deepEqual(release.assets.map(asset => asset.name).sort(), expectedNames);
const checksumAsset = release.assets.find(asset => asset.name === 'SHA256SUMS.txt');
const checksumBytes = await download(checksumAsset.browser_download_url);
await writeFile(join(root, checksumAsset.name), checksumBytes);
const manifest = new Map();
for (const line of checksumBytes.toString('utf8').trim().split('\n')) {
  const match = line.match(/^([a-f0-9]{64})\s+\*?(stow-(?:linux|darwin)-(?:amd64|arm64)\.tar\.gz)$/);
  assert.ok(match, `Unexpected manifest line: ${line}`);
  assert.ok(!manifest.has(match[2]), `Duplicate manifest entry: ${match[2]}`);
  manifest.set(match[2], match[1]);
}
assert.deepEqual([...manifest.keys()].sort(), expectedNames.filter(name => name.endsWith('.tar.gz')));
const assets = [];
for (const asset of release.assets.filter(asset => asset.name.endsWith('.tar.gz'))) {
  const bytes = await download(asset.browser_download_url);
  assert.equal(bytes.length, asset.size);
  const hash = sha256(bytes);
  assert.equal(hash, manifest.get(asset.name), asset.name);
  if (asset.digest) assert.equal(asset.digest, `sha256:${hash}`);
  const archive = join(root, asset.name);
  await writeFile(archive, bytes);
  const entries = execFileSync('tar', ['-tzf', archive], { encoding: 'utf8' }).trim().split('\n');
  assert.deepEqual(entries.sort(), ['LICENSE', 'README.md', 'stow-s3']);
  assets.push({ name: asset.name, bytes: bytes.length, sha256: hash, checksumVerified: true, entries });
  console.log(`Verified ${asset.name}`);
}
const nativeRoot = join(root, 'linux-amd64');
await mkdir(nativeRoot, { recursive: true });
execFileSync('tar', ['-xzf', join(root, 'stow-linux-amd64.tar.gz'), '-C', nativeRoot]);
const binary = join(nativeRoot, 'stow-s3');
const version = execFileSync(binary, ['--version'], { encoding: 'utf8' }).trim();
assert.equal(version, 'stow-s3 0.3.0');
const buildInfo = execFileSync('go', ['version', '-m', binary], { encoding: 'utf8' });
assert.ok(buildInfo.includes(`vcs.revision=${expectedCommit}`), buildInfo);
assert.ok(buildInfo.includes('vcs.modified=false'), buildInfo);
const receipt = {
  verifiedAt: new Date().toISOString(), provider: 'railway',
  projectId: process.env.RAILWAY_PROJECT_ID, environmentId: process.env.RAILWAY_ENVIRONMENT_ID,
  serviceId: process.env.RAILWAY_SERVICE_ID, deploymentId: process.env.RAILWAY_DEPLOYMENT_ID,
  tag: release.tag_name, commit: expectedCommit, releaseId: release.id,
  releaseUrl: release.html_url, publishedAt: release.published_at,
  draft: release.draft, prerelease: release.prerelease,
  bodySha256: sha256(Buffer.from(release.body)),
  checksumManifestSha256: sha256(checksumBytes), assets,
  nativeVersion: version, nativeBuildInfo: buildInfo,
  retainedCloudRoot: root, complete: true,
};
await writeFile(join(root, 'release.json'), JSON.stringify(release, null, 2) + '\n');
await writeFile(join(root, 'verification.json'), JSON.stringify(receipt, null, 2) + '\n');
console.log(await readFile(join(root, 'verification.json'), 'utf8'));
