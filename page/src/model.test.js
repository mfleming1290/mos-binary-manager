import test from 'node:test';
import assert from 'node:assert/strict';
import { absolutePath, argumentLines, availableDiscoveries, clone, editApp, generateAppId, newApp, validateBrowse, validateConfig, validateStatus } from './model.js';
import { fixture } from '../preview/mock.js';

test('complete settings and app updates preserve unknown keys without mutating the source', () => {
  const source = fixture().config;
  const changed = editApp(source, source.apps[0].id, { autostart: false });
  assert.equal(changed.preserved.future, true);
  assert.equal(changed.apps[0].futureAppKey, 'preserved');
  assert.equal(source.apps[0].autostart, true);
  changed.preserved.future = false;
  assert.equal(source.preserved.future, true);
});
test('arguments are literal lines, including spaces, shell syntax and interior empty items', () => {
  assert.deepEqual(argumentLines(''), []);
  assert.deepEqual(argumentLines('--name\nhello world\n\n$(whoami); & rm x\n"quoted"'), ['--name', 'hello world', '', '$(whoami); & rm x', '"quoted"']);
  assert.deepEqual(argumentLines('first\r\nlast\r'), ['first', 'last']);
});
test('new apps are stopped by default and discovered paths do not duplicate configured apps', () => {
  assert.deepEqual(newApp('/opt/a b', 'safe-id'), { id: 'safe-id', path: '/opt/a b', name: 'a b', args: [], workdir: '', autostart: false });
  assert.deepEqual(availableDiscoveries(fixture()).map(app => app.name), ['filebrowser', 'rclone']);
});
test('app IDs use native randomUUID with its Crypto receiver when available', () => {
  const crypto = { randomUUID() { assert.equal(this, crypto); return 'native-id'; } };
  assert.equal(generateAppId(crypto), 'native-id');
});
test('app IDs use getRandomValues when randomUUID is unavailable on HTTP origins', () => {
  const crypto = { getRandomValues(bytes) {
    assert.equal(this, crypto);
    assert.equal(bytes.length, 16);
    bytes.set(Array.from({ length: 16 }, (_, index) => index));
    return bytes;
  } };
  assert.equal(generateAppId(crypto), '00010203-0405-4607-8809-0a0b0c0d0e0f');
});
test('app IDs fail clearly rather than falling back to weak randomness', () => {
  for (const crypto of [null, {}]) assert.throws(() => generateAppId(crypto), /browser.*secure random/i);
});
test('validation fails closed on incompatible or incomplete status/settings', () => {
  for (const value of [null, [], {}, { ...fixture().config, schemaVersion: 2 }, { ...fixture().config, revision: '1' }, { ...fixture().config, folder: 'relative' }]) assert.throws(() => validateConfig(value));
  const duplicate = clone(fixture().config); duplicate.apps.push(duplicate.apps[0]);
  assert.throws(() => validateConfig(duplicate));
  assert.throws(() => validateStatus({ ...fixture(), apps: [{}] }));
  assert.throws(() => validateStatus({ ...fixture(), discovered: [{ path: 'relative', name: 'x' }] }));
  assert.throws(() => editApp(fixture().config, 'missing', {}));
});
test('folder browse listing only permits known entry types and absolute paths', () => {
  assert.equal(validateBrowse({ path: '/', parent: null, entries: [] }).path, '/');
  assert.throws(() => validateBrowse({ path: '/', parent: null, entries: [{ name: 'x', path: '/x', type: 'socket' }] }));
  for (const value of ['relative', '/x\0y', '/x\ny', '/x\ry']) assert.equal(absolutePath(value), false);
});
