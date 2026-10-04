// Local, read-only release checks. Never publishes or installs.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { resolve, join } from 'node:path';
import config from '../page/plugin.config.js';

const root = fileURLToPath(new URL('../', import.meta.url));
const json = p => JSON.parse(readFileSync(join(root, p), 'utf8'));
export function validateMetadata({ config, packageData, lock, hub, tag = '', repository = '' }) {
  assert.match(config.version, /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/, 'Use numeric x.y.z, without a v prefix');
  assert.equal(config.name, 'binary-manager');
  assert.equal(packageData.version, config.version, 'Update page/package.json with plugin.config.js');
  assert.equal(lock.version, config.version, 'Update page/package-lock.json');
  assert.equal(lock.packages[''].version, config.version, 'Update lockfile root version');
  if (tag) assert.equal(tag, config.version, 'Release tag must match the committed source version');
  assert.match(hub.repository, /^https:\/\/github\.com\/[A-Za-z0-9][A-Za-z0-9-]*\/mos-binary-manager$/);
  if (repository) assert.equal(hub.repository, `https://github.com/${repository}`, 'Hub entry points at a different repository');
  assert.equal(config.homepage, hub.repository);
  assert.equal(config.support, `${hub.repository}/issues`);
  assert.equal(hub.homepage, config.homepage);
  assert.equal(hub.support, config.support);
  assert.equal(hub.readme_url, config.homepage);
  assert.equal(hub.name, config.displayName);
  assert.equal(hub.author, config.author);
  assert.deepEqual(hub.architecture, ['amd64', 'arm64']);
  assert.deepEqual(hub.category, ['Utilities']);
  for (const value of [config, hub]) {
    assert.equal(value.driver, false);
    assert.equal(value.settings, true);
  }
  assert.equal(config.widget, false);
  assert.equal(config.icon, '/plugins/binary-manager/icon.svg');
  assert.match(hub.icon, /^https:\/\/raw\.githubusercontent\.com\/[A-Za-z0-9][A-Za-z0-9-]*\/mos-hub\/[^/]+\/images\/binary-managerIcon\.svg$/);
  return config.version;
}

export function checkArtifacts(directory, version) {
  const expected = ['amd64', 'arm64'].map(arch => `binary-manager_${version}-1+mos-plugin_${arch}.deb`);
  assert.deepEqual(readdirSync(directory).filter(f => f.endsWith('.deb')).sort(), expected, 'Exactly one Debian package per supported architecture is required');
  for (const name of expected) {
    const path = join(directory, name);
    const bytes = readFileSync(path);
    for (const algorithm of ['md5', 'sha256']) {
      assert.equal(readFileSync(`${path}.${algorithm}`, 'utf8'), `${createHash(algorithm).update(bytes).digest('hex')}  ${name}\n`, `${name}: invalid ${algorithm}`);
    }
    assert.equal(execFileSync('dpkg-deb', ['-f', path, 'Package'], {encoding:'utf8'}).trim(), 'binary-manager-plugin');
    assert.equal(execFileSync('dpkg-deb', ['-f', path, 'Version'], {encoding:'utf8'}).trim(), `${version}-1+mos-plugin`);
    assert.equal(execFileSync('dpkg-deb', ['-f', path, 'Architecture'], {encoding:'utf8'}).trim(), name.endsWith('_amd64.deb') ? 'amd64' : 'arm64');
  }
}

function main() {
  const flags = new Set(process.argv.slice(2));
  for (const flag of flags) assert.ok(['--artifacts', '--git'].includes(flag), `Unknown flag: ${flag}`);
  const version = validateMetadata({config, packageData: json('page/package.json'), lock: json('page/package-lock.json'), hub: json('hub/plugins/binary-manager.json'), tag: process.env.RELEASE_TAG || '', repository: process.env.GITHUB_REPOSITORY || ''});
  for (const file of ['functions', 'settings.json', 'page/plugin.config.js', 'page/package-lock.json', '.github/workflows/build-plugin.yml', 'hub/images/binary-managerIcon.svg']) assert.ok(existsSync(join(root, file)), `Missing ${file}`);
  const source = readFileSync(join(root, 'page/plugin.config.js'), 'utf8');
  // MOS regex-reads these literal fields from the tag's source archive.
  for (const key of ['name', 'version']) assert.equal(source.match(new RegExp(`${key}:\\s*['\"]([^'\"]+)['\"]`))?.[1], config[key]);
  if (flags.has('--artifacts')) checkArtifacts(join(root, 'artifacts'), version);
  if (flags.has('--git')) {
    const files = execFileSync('git', ['ls-tree', '-rz', '--name-only', 'HEAD'], {cwd:root, encoding:'utf8'}).split('\0').filter(Boolean);
    for (const path of files) assert.ok(!/(^|\/)(node_modules|dist|preview-dist|artifacts|checksums|__pycache__)(\/|$)/.test(path), `Generated output must not be committed: ${path}`);
    for (const path of ['functions', 'settings.json', 'page/plugin.config.js', 'page/package-lock.json', '.github/workflows/build-plugin.yml']) assert.ok(files.includes(path), `${path} is missing from the tagged source`);
    const archive = execFileSync('git', ['archive', '--format=tar.gz', '--prefix=source/', 'HEAD'], {cwd:root, maxBuffer:20*1024*1024});
    assert.ok(archive.length < 10*1024*1024, 'Tagged source archive exceeds the MOS 10 MiB limit');
    console.log(`Tagged source archive: ${archive.length} bytes`);
  }
  console.log(`Release contract OK: ${version}; amd64 + arm64`);
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
