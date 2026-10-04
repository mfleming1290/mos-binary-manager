import test from 'node:test';
import assert from 'node:assert/strict';
import { absolutePath, argumentLines, availableDiscoveries, clone, defaultsTemplate, duplicateApp, duplicateName, editApp, environmentLines, environmentText, generateAppId, newApp, pathDirectoryLines, runtimePath, runtimeTemplate, settingsForm, settingsFromForm, validateBrowse, validateConfig, validateStatus } from './model.js';
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
test('new apps are stopped by default and discoveries allow multiple instances of the same executable', () => {
  assert.deepEqual(newApp('/opt/a b', 'safe-id'), { id: 'safe-id', path: '/opt/a b', name: 'a b', args: [], workdir: '', autostart: false });
  assert.deepEqual(availableDiscoveries(fixture()).map(app => app.name), ['syncthing', 'filebrowser', 'rclone']);
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

test('runtime settings are optional and legacy edits never opt in implicitly', () => {
  const original = fixture().config;
  assert.equal('runtimeDefaults' in validateConfig(original), false);
  const app = newApp('/opt/same', 'new-id');
  assert.equal('runtime' in app, false);
  assert.doesNotThrow(() => validateConfig({ ...original, runtimeDefaults: {}, apps: [...original.apps, { ...app, runtime: {} }] }));
  assert.equal(settingsForm({}, runtimeTemplate).useDefaults, false);
});
test('runtime defaults and nested instance extensions survive form editing without mutation', () => {
  const original = { ...runtimeTemplate(), env: { LONG: ' space = literal ', EMPTY: '', REMOVED: null }, future: { inner: { preserve: true } } };
  const form = settingsForm(original, runtimeTemplate);
  form.homeMode = 'custom'; form.home = '/mnt/pool/home';
  const changed = settingsFromForm(form, original, ['homeMode', 'home']);
  assert.deepEqual(changed.env, original.env);
  assert.deepEqual(changed.future, original.future);
  changed.future.inner.preserve = false;
  assert.equal(original.future.inner.preserve, true);
  const defaults = { ...defaultsTemplate(), future: { nested: 7 } };
  const defaultsForm = settingsForm(defaults, defaultsTemplate);
  defaultsForm.storageRoot = '/mnt/pool/instances';
  defaultsForm.pathDirsText = '/opt/first\n/mnt/pool/bin'; defaultsForm.pathsEdited = true;
  defaultsForm.environmentText = 'MODE=shared\n!DISABLED'; defaultsForm.environmentEdited = true;
  const result = settingsFromForm(defaultsForm, defaults, ['storageRoot']);
  assert.deepEqual(result, { storageRoot: '/mnt/pool/instances', pathDirs: ['/opt/first', '/mnt/pool/bin'], env: { MODE: 'shared', DISABLED: null }, future: { nested: 7 } });
});
test('environment lines distinguish empty values from unsets and never evaluate shell syntax', () => {
  assert.deepEqual(environmentLines(''), {});
  const text = 'MODE= value with spaces \nEMPTY=\n!REMOVE\nCOMMAND=$(id); & "literal"\nEQUAL=a=b=c\r\n';
  const env = { MODE: ' value with spaces ', EMPTY: '', REMOVE: null, COMMAND: '$(id); & "literal"', EQUAL: 'a=b=c' };
  assert.deepEqual(environmentLines(text), env);
  assert.deepEqual(environmentLines(environmentText(env)), env);
  const special = environmentLines('__proto__=literal');
  assert.equal(Object.getPrototypeOf(special), Object.prototype);
  assert.equal(special.__proto__, 'literal');
});
test('environment parsing rejects reserved keys, invalid keys and duplicates without echoing values', () => {
  for (const text of ['HOME=/root', 'PATH=/bin', 'XDG_CACHE_HOME=/cache', 'XDG_FUTURE=value', 'BINARY_MANAGER_ROOT=/tmp', '!BINARY_MANAGER_X', 'export KEY=value', '2BAD=x', 'BAD-NAME=x', 'KEY', 'KEY=a\n!KEY', 'KEY=a\nKEY=b', 'KEY=contains\0nul']) assert.throws(() => environmentLines(text));
  assert.throws(() => environmentLines('SECRET-KEY=do-not-echo'), error => !error.message.includes('do-not-echo'));
});
test('runtime path fields reject relative, unclean and colon-separated PATH entries', () => {
  assert.deepEqual(pathDirectoryLines('/opt/bin\r\n\n/mnt/pool/bin with spaces'), ['/opt/bin', '/mnt/pool/bin with spaces']);
  for (const path of ['relative', '$HOME/bin', '~/bin', '/a/../b', '/a/./b', '//a', '/a/', '/a\tb', '/a\0b']) assert.equal(runtimePath(path), false);
  assert.equal(runtimePath('/'), true);
  for (const text of ['/bin:/usr/bin', '/opt/bin\nrelative']) assert.throws(() => pathDirectoryLines(text));
});
test('new runtime schema rejects malformed optional fields and accepts omitted subfields', () => {
  const config = fixture().config;
  for (const runtimeDefaults of [null, [], '', { storageRoot: null }, { storageRoot: 'relative' }, { storageRoot: '/' }, { pathDirs: null }, { pathDirs: ['/bin:/opt/bin'] }, { env: [] }, { env: { KEY: 7 } }, { env: { KEY: 'two\nlines' } }, { env: { KEY: 'two\rlines' } }]) assert.throws(() => validateConfig({ ...config, runtimeDefaults }));
  for (const runtime of [null, [], { useDefaults: null }, { useDefaults: 'true' }, { homeMode: 'wrong' }, { homeMode: null }, { home: null }, { home: '/' }, { envFile: false }, { xdgDataHome: 'relative' }, { env: { PATH: '/bin' } }, { homeMode: 'managed' }, { homeMode: 'custom', home: '' }]) assert.throws(() => editApp(config, config.apps[0].id, { runtime }));
  assert.doesNotThrow(() => editApp(config, config.apps[0].id, { runtime: {} }));
  assert.doesNotThrow(() => editApp({ ...config, runtimeDefaults: { storageRoot: '/mnt/pool/instances' } }, config.apps[0].id, { runtime: { homeMode: 'managed', useDefaults: false } }));
});
test('duplicate isolates identity, HOME and protected file references while staying off', () => {
  const config = fixture().config;
  config.runtimeDefaults = { storageRoot: '/mnt/pool/instances', futureDefaults: { keep: true } };
  config.apps[0].runtime = { ...runtimeTemplate(), useDefaults: false, homeMode: 'custom', home: '/mnt/pool/original', envFile: '/mnt/pool/private/original.env', pathDirs: ['/opt/bin'], env: { COLOR: 'blue', REMOVE: null }, xdgConfigHome: '/mnt/pool/config', xdgDataHome: '/mnt/pool/data', xdgCacheHome: '/mnt/pool/cache', futureRuntime: { keep: true } };
  const before = clone(config);
  const result = duplicateApp(config, config.apps[0].id, 'fresh-copy-id');
  const copied = result.apps.at(-1);
  assert.equal(copied.id, 'fresh-copy-id'); assert.equal(copied.name, 'Syncthing copy'); assert.equal(copied.autostart, false);
  assert.equal(copied.path, config.apps[0].path); assert.deepEqual(copied.args, config.apps[0].args);
  assert.equal(copied.runtime.homeMode, 'managed'); assert.equal(copied.runtime.useDefaults, false);
  for (const key of ['home', 'envFile', 'xdgConfigHome', 'xdgDataHome', 'xdgCacheHome']) assert.equal(copied.runtime[key], '');
  assert.deepEqual(copied.runtime.env, config.apps[0].runtime.env);
  assert.deepEqual(copied.runtime.futureRuntime, { keep: true }); assert.equal(copied.futureAppKey, 'preserved');
  assert.deepEqual(result.runtimeDefaults, config.runtimeDefaults); assert.deepEqual(config, before);
  assert.equal('desired' in copied, false); assert.equal('running' in copied, false); assert.equal('pid' in copied, false);
  copied.runtime.env.COLOR = 'red'; assert.equal(config.apps[0].runtime.env.COLOR, 'blue');
});
test('duplicate requires storage, rejects ID collisions and legacy copies explicitly opt in', () => {
  const config = fixture().config;
  assert.throws(() => duplicateApp(config, config.apps[0].id, 'copy'), /storage root/);
  config.runtimeDefaults = { storageRoot: '/mnt/pool/instances' };
  assert.throws(() => duplicateApp(config, config.apps[0].id, config.apps[1].id));
  assert.throws(() => duplicateApp(config, 'missing', 'copy'));
  const result = duplicateApp(config, config.apps[0].id);
  assert.notEqual(result.apps.at(-1).id, config.apps[0].id);
  assert.match(result.apps.at(-1).id, /^[0-9a-f-]{36}$/);
  assert.equal(result.apps.at(-1).runtime.useDefaults, true);
  assert.equal(result.apps.at(-1).runtime.homeMode, 'managed');
  assert.equal('runtime' in result.apps[0], false);
});
test('duplicate names fit the host UTF-8 byte limit without splitting a character', () => {
  for (const original of ['x'.repeat(160), '🌲'.repeat(40), 'é'.repeat(80)]) {
    const name = duplicateName(original);
    assert.ok(Buffer.byteLength(name) <= 160); assert.ok(name.endsWith(' copy')); assert.ok(!name.includes('\ufffd'));
  }
  assert.equal(duplicateName('Server'), 'Server copy');
});

test('environment and PATH counts and UTF-8 byte limits align with the host', () => {
  assert.throws(() => environmentLines(Array.from({ length: 257 }, (_, i) => `KEY_${i}=x`).join('\n')), /256/);
  assert.throws(() => environmentLines(`${'K'.repeat(129)}=x`));
  assert.throws(() => environmentLines(`VALUE=${'é'.repeat(8193)}`), /16 KiB/);
  assert.throws(() => pathDirectoryLines(Array.from({ length: 65 }, (_, i) => `/opt/${i}`).join('\n')), /64/);
  assert.equal(runtimePath('/' + 'é'.repeat(2048)), false);
});
