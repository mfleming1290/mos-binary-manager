import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { validateMetadata } from '../scripts/check-release.mjs';
import config from '../page/plugin.config.js';
const json = p => JSON.parse(readFileSync(new URL(p, import.meta.url), 'utf8'));
const wrongVersion = config.version === '0.0.0' ? '0.0.1' : '0.0.0';
const fixture = () => structuredClone({ config, packageData: json('../page/package.json'), lock: json('../page/package-lock.json'), hub: json('../hub/plugins/binary-manager.json'), tag: config.version, repository: 'mfleming1290/mos-binary-manager' });
test('Hub, tag, source and lockfile metadata agree', () => assert.equal(validateMetadata(fixture()), config.version));
test('wrong or v-prefixed tags cannot publish stale source metadata', () => {
  for (const tag of [`v${config.version}`, wrongVersion, `${config.version};echo unsafe`]) assert.throws(() => validateMetadata({...fixture(), tag}));
});
test('wrong repository and architecture cannot masquerade as a compatible release', () => {
  assert.throws(() => validateMetadata({...fixture(), repository: 'different/mos-binary-manager'}));
  const f = fixture(); f.hub.architecture = ['all']; assert.throws(() => validateMetadata(f));
});
test('package and lockfile versions must both match the source config', () => {
  for (const key of ['packageData', 'lock']) { const f = fixture(); f[key].version = wrongVersion; assert.throws(() => validateMetadata(f)); }
  const f = fixture(); f.lock.packages[''].version = wrongVersion; assert.throws(() => validateMetadata(f));
});
test('Hub presentation links and settings stay aligned', () => {
  const f = fixture(); f.hub.settings = false; assert.throws(() => validateMetadata(f));
  const g = fixture(); g.hub.homepage = ''; assert.throws(() => validateMetadata(g));
});
