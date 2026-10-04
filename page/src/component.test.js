import test, { after } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { join } from 'node:path';
import { parse, compileScript } from '@vue/compiler-sfc';
import { JSDOM } from 'jsdom';
import { createMock, fixture, runtimeFixture } from '../preview/mock.js';

const dom = new JSDOM('<!doctype html><html><body></body></html>', { url: 'https://preview.invalid' });
for (const key of ['window', 'document', 'Element', 'HTMLElement', 'SVGElement', 'Node', 'Event', 'MouseEvent', 'CustomEvent']) globalThis[key] = dom.window[key];
globalThis.localStorage = dom.window.localStorage;
localStorage.setItem('authToken', 'fixture-only');
dom.window.HTMLDialogElement.prototype.showModal = function () { this.open = true; };
const { createApp, nextTick, h } = await import('vue');
const { useManager } = await import('./useManager.js');
const root = fileURLToPath(new URL('../', import.meta.url));
const tmp = await mkdtemp(join(root, '.test-components-'));
for (const name of ['Plugin', 'Modal', 'FilePicker']) {
  const source = await readFile(join(root, 'src', name + '.vue'), 'utf8');
  const { descriptor } = parse(source);
  let compiled = compileScript(descriptor, { id: `test-${name}`, inlineTemplate: true }).content;
  compiled = compiled.replace(/(['"])\.\/([^'"]+)\.vue\1/g, (_, quote, path) => `${quote}./${path}.mjs${quote}`);
  compiled = compiled.replace(/(['"])\.\/([^'"]+)\.js\1/g, (_, quote, path) => `${quote}${pathToFileURL(join(root, 'src', path + '.js')).href}${quote}`);
  await writeFile(join(tmp, name + '.mjs'), compiled);
}
const Plugin = (await import(pathToFileURL(join(tmp, 'Plugin.mjs')))).default;
after(async () => { await rm(tmp, { recursive: true, force: true }); dom.window.close(); });
const flush = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); await nextTick(); await new Promise(resolve => setTimeout(resolve, 0)); await nextTick(); };
function mount(mock = createMock()) {
  mock.delay = 0;
  globalThis.fetch = mock.fetch;
  const element = document.createElement('main');
  document.body.append(element);
  const app = createApp(Plugin); app.mount(element);
  return { mock, element, close: () => { app.unmount(); element.remove(); } };
}
const button = (element, text) => [...element.querySelectorAll('button')].find(node => node.textContent.trim() === text);
const byLabel = (element, label) => element.querySelector(`[aria-label="${label}"]`);
const input = (element, value) => { element.value = value; element.dispatchEvent(new Event('input', { bubbles: true })); };
const check = (element, value) => { element.checked = value; element.dispatchEvent(new Event('change', { bubbles: true })); };
const submit = form => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));

test('component loads apps and discoveries without starting anything', async () => {
  const view = mount();
  try { await flush(); assert.match(view.element.textContent, /Syncthing/); assert.match(view.element.textContent, /filebrowser/); assert.deepEqual(view.mock.calls.map(call => call.action), ['status']); assert.equal(byLabel(view.element, 'Keep Syncthing running').checked, true); assert.equal(byLabel(view.element, 'Start Syncthing on boot').checked, true); } finally { view.close(); }
});
test('adding a discovered executable saves it inert and only an explicit switch starts it', async () => {
  const view = mount();
  try {
    await flush(); byLabel(view.element, 'Add filebrowser').click(); await flush();
    const save = view.mock.calls.find(call => call.action === 'save');
    const added = save.config.apps.at(-1);
    assert.equal(added.path, '/mnt/user/applications/filebrowser'); assert.equal(added.autostart, false); assert.deepEqual(added.args, []);
    assert.equal(view.mock.calls.some(call => call.action === 'toggle'), false);
    assert.equal(byLabel(view.element, 'Keep filebrowser running').checked, false);
    check(byLabel(view.element, 'Keep filebrowser running'), true); await flush();
    assert.deepEqual(view.mock.calls.at(-1), { action: 'toggle', id: added.id, enabled: true });
    assert.equal(view.mock.value.config.apps.at(-1).autostart, false);
  } finally { view.close(); }
});
for (const source of ['manual', 'discovered']) test(`adding a ${source} executable works without the secure-context-only randomUUID API`, async () => {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis.crypto, 'randomUUID');
  Object.defineProperty(globalThis.crypto, 'randomUUID', { configurable: true, value: undefined });
  const view = mount();
  try {
    await flush();
    if (source === 'manual') {
      input(view.element.querySelector('#bm-manual'), '/mnt/user/binaries/gomcp');
      submit(view.element.querySelector('#bm-manual').closest('form'));
    } else byLabel(view.element, 'Add filebrowser').click();
    await flush();
    const save = view.mock.calls.find(call => call.action === 'save');
    assert.ok(save, 'Adding must reach the host save request without randomUUID');
    const added = save.config.apps.at(-1);
    assert.match(added.id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    assert.equal(added.path, source === 'manual' ? '/mnt/user/binaries/gomcp' : '/mnt/user/applications/filebrowser');
    assert.equal(added.autostart, false);
    assert.equal(byLabel(view.element, `Keep ${added.name} running`).checked, false);
    assert.equal(view.mock.calls.some(call => call.action === 'toggle'), false);
    assert.equal(view.element.querySelector('[role="alert"]'), null);
  } finally {
    view.close();
    if (descriptor) Object.defineProperty(globalThis.crypto, 'randomUUID', descriptor);
    else delete globalThis.crypto.randomUUID;
  }
});
test('boot preference is independent and failed checkboxes immediately roll back', async () => {
  const view = mount();
  try {
    await flush(); view.mock.failNext = true;
    const keep = byLabel(view.element, 'Keep Restic backup running'); check(keep, true); assert.equal(keep.checked, false); await flush(); assert.equal(keep.checked, false);
    view.mock.failNext = true;
    const boot = byLabel(view.element, 'Start Restic backup on boot'); check(boot, true); assert.equal(boot.checked, false); await flush(); assert.equal(boot.checked, false);
    check(boot, true); await flush(); assert.equal(boot.checked, true); assert.equal(view.mock.value.apps[1].desired, false);
    check(byLabel(view.element, 'Keep Syncthing running'), false); await flush(); assert.equal(view.mock.value.config.apps[0].autostart, true);
  } finally { view.close(); }
});
test('server file picker starts at root, is read-only, and cancel leaves configuration untouched', async () => {
  const view = mount();
  try {
    await flush(); const browseButtons = [...view.element.querySelectorAll('button')].filter(node => node.textContent === 'Browse'); browseButtons[1].click(); await flush();
    assert.equal(view.mock.calls.at(-1).action, 'browse'); assert.equal(view.mock.calls.at(-1).path, '/');
    const dialog = view.element.querySelector('dialog');
    [...dialog.querySelectorAll('li button')].find(node => node.textContent.includes('opt')).click(); await flush();
    [...dialog.querySelectorAll('li button')].find(node => node.textContent.includes('my server')).click(); await flush();
    assert.equal(view.element.querySelector('#bm-manual').value, '/opt/my server'); assert.equal(view.mock.calls.some(call => call.action === 'save'), false);
    browseButtons[0].click(); await flush(); view.element.querySelector('dialog').dispatchEvent(new Event('cancel', { cancelable: true })); await flush();
    assert.equal(view.element.querySelector('dialog'), null); assert.equal(view.mock.calls.some(call => call.action === 'save'), false);
  } finally { view.close(); }
});
test('folder draft uses its original revision and conflict cannot overwrite newer configuration', async () => {
  const view = mount();
  try {
    await flush(); input(view.element.querySelector('#bm-folder'), '/opt'); await flush();
    view.mock.value.config.revision = 9; button(view.element, 'Refresh').click(); await flush();
    submit(view.element.querySelector('#bm-folder').closest('form')); await flush();
    assert.equal(view.mock.calls.find(call => call.action === 'save').expectedRevision, 2);
    assert.equal(view.mock.value.config.revision, 9); assert.equal(view.mock.value.config.folder, '/mnt/user/applications'); assert.match(view.element.textContent, /Configuration changed elsewhere/);
  } finally { view.close(); }
});
test('app editor preserves untouched empty/multiline args and future keys', async () => {
  const data = fixture(); data.config.apps[0].args = ['', 'two\nlines', ' space '];
  const view = mount(createMock(data));
  try {
    await flush(); byLabel(view.element, 'Edit Syncthing').click(); await flush(); input(view.element.querySelector('#bm-edit-name'), 'Syncthing renamed'); submit(view.element.querySelector('#bm-edit-name').closest('form')); await flush();
    const saved = view.mock.calls.find(call => call.action === 'save').config;
    assert.deepEqual(saved.apps[0].args, ['', 'two\nlines', ' space ']); assert.equal(saved.apps[0].futureAppKey, 'preserved'); assert.equal(saved.preserved.future, true);
    byLabel(view.element, 'Edit Syncthing renamed').click(); await flush(); input(view.element.querySelector('#bm-edit-args'), '--name\nspace value\n\n$(id)'); submit(view.element.querySelector('#bm-edit-name').closest('form')); await flush();
    assert.deepEqual(view.mock.value.config.apps[0].args, ['--name', 'space value', '', '$(id)']);
  } finally { view.close(); }
});
test('unverifiable mutation disables controls until explicit status recovery', async () => {
  const view = mount();
  try {
    await flush(); view.mock.failNext = true; view.mock.failStatus = true;
    check(byLabel(view.element, 'Keep Restic backup running'), true); await flush();
    assert.equal(byLabel(view.element, 'Keep Restic backup running').disabled, true); assert.match(view.element.textContent, /could not be verified/);
    view.mock.failStatus = false; button(view.element, 'Retry status').click(); await flush();
    assert.equal(byLabel(view.element, 'Keep Restic backup running').disabled, false); assert.doesNotMatch(view.element.textContent, /could not be verified/);
  } finally { view.close(); }
});
test('remove requires the confirmation dialog and leaves the file as an available discovery', async () => {
  const view = mount();
  try {
    await flush(); byLabel(view.element, 'Remove Syncthing').click(); await flush();
    assert.equal(view.mock.calls.some(call => call.action === 'remove'), false);
    button(view.element, 'Stop and remove').click(); await flush();
    assert.equal(view.mock.calls.at(-1).action, 'remove'); assert.equal(view.mock.calls.at(-1).expectedRevision, 2); assert.equal(view.mock.value.config.apps.length, 1); assert.ok(byLabel(view.element, 'Add syncthing'));
  } finally { view.close(); }
});
test('manager serializes polls, clears recovered polling errors, and aborts requests on unmount', async () => {
  const calls = []; let fail = true; let pending; let signal;
  const send = async (payload, options) => { calls.push(payload); signal = options.signal; if (pending) await pending; if (fail) throw new Error('Offline'); return fixture(); };
  let manager;
  const element = document.createElement('main'); document.body.append(element);
  const app = createApp({ setup() { manager = useManager(send, 100000); return () => h('div'); } }); app.mount(element); await flush();
  assert.equal(manager.stale.value, true); fail = false; await manager.refresh(false); assert.equal(manager.error.value, '');
  let resolve; pending = new Promise(done => { resolve = done; }); const current = manager.refresh(false); await flush();
  const count = calls.length; await manager.refresh(false); assert.equal(calls.length, count);
  app.unmount(); element.remove(); assert.equal(signal.aborted, true); resolve(); await current;
});

test('closing a log request aborts it and late output cannot replace another app selection', async () => {
  const view = mount();
  try {
    await flush();
    const normal = view.mock.fetch;
    let resolveOld; let oldSignal;
    globalThis.fetch = async (url, options) => {
      const body = JSON.parse(options.body);
      const payload = JSON.parse(Buffer.from(body.args[1], 'base64url').toString('utf8'));
      if (payload.action === 'logs' && payload.id === view.mock.value.config.apps[0].id) {
        oldSignal = options.signal;
        return new Promise(resolve => { resolveOld = () => resolve({ ok: true, json: async () => ({ success: true, output: { ok: true, lines: 'OLD APP PRIVATE OUTPUT' }, timed_out: false, exit_code: 0 }) }); });
      }
      return normal(url, options);
    };
    byLabel(view.element, 'View Syncthing logs').click(); await flush();
    button(view.element, 'Close').click(); await flush(); assert.equal(oldSignal.aborted, true);
    byLabel(view.element, 'View Restic backup logs').click(); await flush();
    assert.match(view.element.querySelector('dialog').textContent, /Restic backup/);
    resolveOld(); await flush();
    assert.doesNotMatch(view.element.querySelector('pre').textContent, /OLD APP/);
    assert.match(view.element.querySelector('pre').textContent, /Starting foreground process/);
  } finally { view.close(); }
});
test('nested picker returns to the preserved app draft and accessible dialog labeling is valid', async () => {
  const view = mount();
  try {
    await flush(); byLabel(view.element, 'Edit Syncthing').click(); await flush();
    input(view.element.querySelector('#bm-edit-name'), 'Unsaved draft name');
    let dialog = view.element.querySelector('dialog');
    assert.equal(document.getElementById(dialog.getAttribute('aria-labelledby')).textContent, 'Edit Syncthing');
    button(dialog, 'Browse').click(); await flush();
    assert.equal(view.element.querySelectorAll('dialog').length, 1);
    button(view.element.querySelector('dialog'), 'Cancel').click(); await flush();
    assert.equal(view.element.querySelector('#bm-edit-name').value, 'Unsaved draft name');
    assert.equal(view.mock.calls.some(call => call.action === 'save'), false);
  } finally { view.close(); }
});
test('bounded discovery and browse responses disclose hidden entries', async () => {
  const data = fixture(); data.discoveryTruncated = true;
  const view = mount(createMock(data));
  try {
    await flush(); assert.match(view.element.textContent, /Some files are not shown/);
    const normal = view.mock.handle;
    view.mock.handle = payload => { const result = normal(payload); return payload.action === 'browse' ? { ...result, truncated: true } : result; };
    button(view.element, 'Browse').click(); await flush(); assert.match(view.element.querySelector('dialog').textContent, /Some entries are hidden/);
  } finally { view.close(); }
});
test('automatic recovery replaces outdated mutation-disabled wording without hiding uncertainty', async () => {
  let statusFail = false;
  let manager;
  const send = async payload => {
    if (payload.action !== 'status') throw new Error('Connection lost');
    if (statusFail) throw new Error('Offline');
    return fixture();
  };
  const element = document.createElement('main'); document.body.append(element);
  const app = createApp({ setup() { manager = useManager(send, 100000); return () => h('div'); } }); app.mount(element);
  try {
    await flush(); statusFail = true; await manager.mutate({ action: 'toggle', id: 'x', enabled: true });
    assert.match(manager.error.value, /disabled until a refresh succeeds/);
    statusFail = false; await manager.refresh(false);
    assert.equal(manager.disabled.value, false); assert.match(manager.error.value, /Status is current now/); assert.doesNotMatch(manager.error.value, /disabled until/);
  } finally { app.unmount(); element.remove(); }
});

const applySettings = element => button(element, 'Apply & restart affected running instances');
const select = (element, value) => { element.value = value; element.dispatchEvent(new Event('change', { bubbles: true })); };


test('manual add and discovery can add multiple stopped instances of an existing executable', async () => {
  const view = mount();
  try {
    await flush(); const path = view.mock.value.config.apps[0].path;
    input(view.element.querySelector('#bm-manual'), path); submit(view.element.querySelector('#bm-manual').closest('form')); await flush();
    assert.equal(view.mock.value.config.apps.filter(app => app.path === path).length, 2);
    byLabel(view.element, 'Add syncthing').click(); await flush();
    const matches = view.mock.value.config.apps.filter(app => app.path === path);
    assert.equal(matches.length, 3); assert.equal(new Set(matches.map(app => app.id)).size, 3);
    for (const app of matches.slice(1)) { assert.equal(app.autostart, false); assert.equal('runtime' in app, false); assert.equal(view.mock.value.apps.find(state => state.id === app.id).desired, false); }
    assert.ok(byLabel(view.element, 'Add syncthing')); assert.equal(view.mock.calls.some(call => call.action === 'toggle'), false);
  } finally { view.close(); }
});
test('duplicate starts with separate managed HOME, new secure ID and cleared private references', async () => {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis.crypto, 'randomUUID');
  Object.defineProperty(globalThis.crypto, 'randomUUID', { configurable: true, value: undefined });
  const view = mount(createMock(runtimeFixture()));
  try {
    await flush(); const original = structuredClone(view.mock.value.config.apps[0]);
    byLabel(view.element, 'Duplicate Syncthing').click(); byLabel(view.element, 'Duplicate Syncthing').click(); await flush();
    assert.equal(view.mock.calls.filter(call => call.action === 'save').length, 1);
    const copy = view.mock.value.config.apps.at(-1);
    assert.match(copy.id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    assert.notEqual(copy.id, original.id); assert.equal(copy.name, 'Syncthing copy'); assert.equal(copy.autostart, false);
    assert.deepEqual(copy.runtime.env, original.runtime.env); assert.deepEqual(copy.args, original.args);
    assert.equal(copy.runtime.homeMode, 'managed'); assert.equal(copy.runtime.useDefaults, false);
    for (const key of ['home', 'envFile', 'xdgConfigHome', 'xdgDataHome', 'xdgCacheHome']) assert.equal(copy.runtime[key], '');
    assert.equal(byLabel(view.element, 'Keep Syncthing copy running').checked, false); assert.equal(byLabel(view.element, 'Start Syncthing copy on boot').checked, false);
    assert.equal(view.mock.calls.some(call => call.action === 'toggle' || call.action === 'restart'), false);
    assert.deepEqual(view.mock.value.config.apps[0], original); assert.match(view.element.textContent, /Protected environment file.*were not copied/);
  } finally { view.close(); if (descriptor) Object.defineProperty(globalThis.crypto, 'randomUUID', descriptor); else delete globalThis.crypto.randomUUID; }
});
test('duplicate without storage root opens a blank defaults form and cancel has no side effects', async () => {
  const view = mount();
  try {
    await flush(); byLabel(view.element, 'Duplicate Syncthing').click(); await flush();
    assert.equal(view.element.querySelector('#bm-storage-root').value, '');
    assert.match(view.element.querySelector('dialog').textContent, /Choose a persistent storage root first/);
    button(view.element.querySelector('dialog'), 'Cancel').click(); await flush();
    assert.equal(view.element.querySelector('dialog'), null); assert.equal(view.mock.calls.some(call => call.action === 'save'), false);
    assert.equal(view.mock.value.config.apps.length, 2);
  } finally { view.close(); }
});
test('saving shared defaults preserves unknown fields and legacy behavior, without implicitly duplicating', async () => {
  const view = mount(createMock(runtimeFixture()));
  try {
    await flush(); button(view.element, 'Edit runtime defaults').click(); await flush();
    input(view.element.querySelector('#bm-storage-root'), '/mnt/preview-pool/new-root');
    input(view.element.querySelector('#bm-default-path'), '/opt/tools\n/mnt/preview-pool/shared');
    input(view.element.querySelector('#bm-default-env'), 'NEW= literal $(id) \nEMPTY=\n!REMOVE');
    applySettings(view.element.querySelector('dialog')).click(); await flush();
    const saved = view.mock.calls.find(call => call.action === 'save');
    assert.equal(saved.expectedRevision, 2); assert.equal(saved.config.runtimeDefaults.storageRoot, '/mnt/preview-pool/new-root');
    assert.deepEqual(saved.config.runtimeDefaults.pathDirs, ['/opt/tools', '/mnt/preview-pool/shared']);
    assert.deepEqual(saved.config.runtimeDefaults.env, { NEW: ' literal $(id) ', EMPTY: '', REMOVE: null });
    assert.deepEqual(saved.config.runtimeDefaults.futureDefaults, { keep: true });
    assert.equal('runtime' in saved.config.apps[1], false); assert.equal(saved.config.apps.length, 2);
    assert.equal(view.mock.value.apps[1].desired, false); assert.equal(view.mock.calls.some(call => call.action === 'restart' || call.action === 'toggle'), false);
    assert.equal(view.element.querySelector('dialog'), null);
  } finally { view.close(); }
});
test('runtime defaults validation stays local and does not discard the draft', async () => {
  const view = mount();
  try {
    await flush(); button(view.element, 'Edit runtime defaults').click(); await flush();
    input(view.element.querySelector('#bm-storage-root'), '/mnt/preview-pool/root');
    input(view.element.querySelector('#bm-default-env'), 'HOME=must-not-be-echoed');
    applySettings(view.element.querySelector('dialog')).click(); await flush();
    assert.equal(view.mock.calls.some(call => call.action === 'save'), false); assert.match(view.element.querySelector('[role="alert"]').textContent, /reserved|dedicated/);
    assert.doesNotMatch(view.element.querySelector('[role="alert"]').textContent, /must-not-be-echoed/);
    assert.equal(view.element.querySelector('#bm-storage-root').value, '/mnt/preview-pool/root');
    input(view.element.querySelector('#bm-default-env'), 'MODE=ok'); applySettings(view.element.querySelector('dialog')).click(); await flush();
    assert.equal(view.mock.value.config.runtimeDefaults.env.MODE, 'ok');
  } finally { view.close(); }
});
test('storage help distinguishes filesystem types from pool names and accepts native-pool paths', async () => {
  const view = mount(createMock(runtimeFixture()));
  try {
    await flush(); button(view.element, 'Edit runtime defaults').click(); await flush();
    let dialog = view.element.querySelector('dialog');
    assert.match(dialog.textContent, /dedicated private directory/);
    assert.match(dialog.textContent, /Btrfs pool can also hold Docker or LXC data in separate directories/);
    assert.match(dialog.textContent, /filesystem, not the pool name/);
    assert.doesNotMatch(dialog.textContent, /mergerfs.*such as \/mnt\/user/i);
    input(view.element.querySelector('#bm-storage-root'), '/mnt/user/binary-manager');
    applySettings(dialog).click(); await flush();
    assert.equal(view.mock.value.config.runtimeDefaults.storageRoot, '/mnt/user/binary-manager');
    byLabel(view.element, 'Edit Syncthing').click(); await flush();
    select(view.element.querySelector('#bm-home-mode'), 'custom'); await flush();
    dialog = view.element.querySelector('dialog');
    assert.match(dialog.textContent, /filesystem, not the pool name/);
    assert.doesNotMatch(dialog.textContent, /mergerfs.*such as \/mnt\/user/i);
    button(dialog, 'Cancel').click(); await flush();
    assert.equal(view.element.querySelector('dialog'), null);
  } finally { view.close(); }
});
test('instance runtime saves literal overrides and dedicated paths while preserving ID and nested fields', async () => {
  const view = mount(createMock(runtimeFixture()));
  try {
    await flush(); const id = view.mock.value.config.apps[0].id;
    byLabel(view.element, 'Edit Syncthing').click(); await flush();
    assert.equal(view.element.querySelector('#bm-runtime-enabled').checked, true); assert.equal(view.element.querySelector('#bm-use-defaults').checked, false);
    check(view.element.querySelector('#bm-use-defaults'), true); select(view.element.querySelector('#bm-home-mode'), 'managed');
    input(view.element.querySelector('#bm-runtime-path'), '/opt/first\n/mnt/preview-pool/bin');
    input(view.element.querySelector('#bm-runtime-env'), 'MODE= instance $(literal) \nEMPTY=\n!SHARED');
    input(view.element.querySelector('#bm-env-file'), '/mnt/preview-pool/private/new.env');
    input(view.element.querySelector('#bm-xdg-config'), '/mnt/preview-pool/new-config');
    assert.match(view.element.querySelector('dialog').textContent, /visible in this browser/);
    assert.match(view.element.querySelector('dialog').textContent, /working directory is separate from HOME/);
    applySettings(view.element.querySelector('dialog')).click(); await flush();
    const saved = view.mock.value.config.apps[0]; assert.equal(saved.id, id); assert.equal(saved.runtime.useDefaults, true); assert.equal(saved.runtime.homeMode, 'managed');
    assert.deepEqual(saved.runtime.env, { MODE: ' instance $(literal) ', EMPTY: '', SHARED: null });
    assert.deepEqual(saved.runtime.pathDirs, ['/opt/first', '/mnt/preview-pool/bin']); assert.equal(saved.runtime.envFile, '/mnt/preview-pool/private/new.env');
    assert.equal(saved.runtime.xdgConfigHome, '/mnt/preview-pool/new-config'); assert.deepEqual(saved.runtime.futureRuntime, { keep: true });
    assert.equal(saved.futureAppKey, 'preserved'); assert.equal(view.mock.calls.some(call => call.action !== 'status' && call.action !== 'save'), false);
  } finally { view.close(); }
});
test('runtime opt-in and opt-out are explicit and cancel never changes either', async () => {
  const view = mount();
  try {
    await flush(); byLabel(view.element, 'Edit Restic backup').click(); await flush();
    assert.equal(view.element.querySelector('#bm-runtime-enabled').checked, false); check(view.element.querySelector('#bm-runtime-enabled'), true); await flush();
    assert.equal(view.element.querySelector('#bm-home-mode').value, 'inherit');
    input(view.element.querySelector('#bm-runtime-env'), 'COLOR=blue'); button(view.element.querySelector('dialog'), 'Cancel').click(); await flush();
    assert.equal('runtime' in view.mock.value.config.apps[1], false); assert.equal(view.mock.calls.some(call => call.action === 'save'), false);
    byLabel(view.element, 'Edit Restic backup').click(); await flush(); check(view.element.querySelector('#bm-runtime-enabled'), true); await flush();
    input(view.element.querySelector('#bm-runtime-env'), 'COLOR=green'); applySettings(view.element.querySelector('dialog')).click(); await flush();
    assert.equal(view.mock.value.config.apps[1].runtime.env.COLOR, 'green'); assert.equal(view.mock.value.apps[1].desired, false);
    byLabel(view.element, 'Edit Restic backup').click(); await flush(); check(view.element.querySelector('#bm-runtime-enabled'), false); await flush();
    applySettings(view.element.querySelector('dialog')).click(); await flush(); assert.equal('runtime' in view.mock.value.config.apps[1], false);
  } finally { view.close(); }
});
for (const kind of ['instance', 'defaults']) test(`${kind} settings keep a failed CAS draft visible and require explicit reopen`, async () => {
  const view = mount(createMock(runtimeFixture()));
  try {
    await flush();
    if (kind === 'instance') byLabel(view.element, 'Edit Syncthing').click(); else button(view.element, 'Edit runtime defaults').click();
    await flush(); const selector = kind === 'instance' ? '#bm-runtime-env' : '#bm-default-env';
    input(view.element.querySelector(selector), 'LOCAL=unsaved');
    if (kind === 'instance') view.mock.value.config.apps[0].runtime.env = { REMOTE: 'newer' }; else view.mock.value.config.runtimeDefaults.env = { REMOTE: 'newer' };
    view.mock.value.config.revision = 11;
    button(view.element, 'Refresh').click(); await flush();
    applySettings(view.element.querySelector('dialog')).click(); await flush();
    assert.equal(view.mock.calls.find(call => call.action === 'save').expectedRevision, 2);
    assert.equal(view.mock.value.config.revision, 11); assert.equal(view.element.querySelector(selector).value, 'LOCAL=unsaved');
    assert.equal(applySettings(view.element.querySelector('dialog')).disabled, true);
    assert.match(view.element.querySelector('dialog').textContent, /Close and reopen/);
    submit(view.element.querySelector('dialog form')); await flush(); assert.equal(view.mock.calls.filter(call => call.action === 'save').length, 1);
    button(view.element.querySelector('dialog'), 'Cancel').click(); await flush();
    if (kind === 'instance') byLabel(view.element, 'Edit Syncthing').click(); else button(view.element, 'Edit runtime defaults').click();
    await flush(); assert.equal(view.element.querySelector(selector).value, 'REMOTE=newer'); assert.equal(applySettings(view.element.querySelector('dialog')).disabled, false);
  } finally { view.close(); }
});
test('stale host status prevents duplicates and applying runtime drafts until refresh', async () => {
  const view = mount(createMock(runtimeFixture()));
  try {
    await flush(); button(view.element, 'Edit runtime defaults').click(); await flush();
    input(view.element.querySelector('#bm-default-env'), 'LOCAL=kept'); view.mock.failStatus = true;
    button(view.element, 'Refresh').click(); await flush();
    assert.equal(byLabel(view.element, 'Duplicate Syncthing').disabled, true); assert.equal(applySettings(view.element.querySelector('dialog')).disabled, true);
    submit(view.element.querySelector('dialog form')); await flush(); assert.equal(view.mock.calls.some(call => call.action === 'save'), false);
    view.mock.failStatus = false; button(view.element, 'Retry status').click(); await flush();
    assert.equal(view.element.querySelector('#bm-default-env').value, 'LOCAL=kept'); assert.equal(applySettings(view.element.querySelector('dialog')).disabled, false);
    applySettings(view.element.querySelector('dialog')).click(); await flush(); assert.deepEqual(view.mock.value.config.runtimeDefaults.env, { LOCAL: 'kept' });
  } finally { view.close(); }
});
test('nested picker cancellation preserves runtime draft and log warning covers secrets', async () => {
  const view = mount(createMock(runtimeFixture()));
  try {
    await flush(); byLabel(view.element, 'Edit Syncthing').click(); await flush();
    input(view.element.querySelector('#bm-runtime-env'), 'LOCAL=preserve');
    button(view.element.querySelector('dialog'), 'Browse').click(); await flush(); button(view.element.querySelector('dialog'), 'Cancel').click(); await flush();
    assert.equal(view.element.querySelector('#bm-runtime-env').value, 'LOCAL=preserve');
    button(view.element.querySelector('dialog'), 'Cancel').click(); await flush(); assert.equal(view.mock.calls.some(call => call.action === 'save'), false);
    byLabel(view.element, 'View Syncthing logs').click(); await flush(); assert.match(view.element.querySelector('dialog').textContent, /may contain secrets/);
  } finally { view.close(); }
});

// Run the production scheduled callback explicitly, without sleeping for three
// seconds or changing its interval. Other timers retain their normal behavior.
function capturePolling() {
  const original = globalThis.setTimeout;
  let poll;
  globalThis.setTimeout = (callback, delay, ...args) => {
    const timer = original(callback, delay, ...args);
    if (delay === 3000) poll = () => { clearTimeout(timer); callback(...args); };
    return timer;
  };
  return { run: () => { assert.ok(poll, 'A background poll must be scheduled'); const callback = poll; poll = null; callback(); }, restore: () => { globalThis.setTimeout = original; } };
}

const pollDrafts = [
  { name: 'folder', selector: '#bm-folder', draft: '/mnt/preview-pool/unsaved-folder', open: () => {}, save: element => button(element, 'Save folder') },
  { name: 'manual executable', selector: '#bm-manual', draft: '/opt/unsaved-executable', open: () => {}, save: element => button(element, 'Add app') },
  { name: 'instance settings', selector: '#bm-edit-args', draft: '--unsaved\nvalue with spaces', open: element => byLabel(element, 'Edit Syncthing').click(), save: element => applySettings(element.querySelector('dialog')) },
  { name: 'runtime defaults', selector: '#bm-storage-root', draft: '/mnt/preview-pool/unsaved-root', open: element => button(element, 'Edit runtime defaults').click(), save: element => applySettings(element.querySelector('dialog')) },
];
for (const scenario of pollDrafts) test(`background polling keeps ${scenario.name} editable without replacing its draft or CAS baseline`, async () => {
  const polling = capturePolling();
  const view = mount(createMock(runtimeFixture()));
  let release;
  try {
    await flush(); scenario.open(view.element); await flush();
    const field = view.element.querySelector(scenario.selector);
    input(field, scenario.draft); field.focus(); field.setSelectionRange(2, 5); await flush();
    const fields = [...field.closest('fieldset').querySelectorAll('input, textarea, select')];
    const state = () => fields.map(node => ({ id: node.id, value: node.value, checked: node.checked }));
    const draftState = state();
    const assertDraft = () => {
      // jsdom does not blur a disabled fieldset like real browsers do: effective
      // disabled state catches the cause, while identity/focus/selection cover
      // remounts and text replacement separately.
      for (const node of fields) {
        assert.equal(node.matches(':disabled'), false, `${node.id} must remain editable during status polling`);
        assert.equal(view.element.querySelector(`#${node.id}`), node);
      }
      assert.deepEqual(state(), draftState);
      assert.equal(document.activeElement, field);
      assert.deepEqual([field.selectionStart, field.selectionEnd], [2, 5]);
    };
    const normalFetch = globalThis.fetch;
    globalThis.fetch = async (...args) => { await new Promise(resolve => { release = resolve; }); return normalFetch(...args); };
    // Check unchanged and newer snapshots, then submit during one more poll
    // after revision 9 is loaded. None may change the draft or its CAS baseline.
    for (const changed of [false, true, false]) {
      polling.run(); await flush();
      assertDraft(); assert.equal(scenario.save(view.element).disabled, true);
      // Enter/form submission must not sneak a write past the disabled button.
      submit(field.closest('form')); await flush();
      assert.equal(view.mock.calls.some(call => call.action === 'save'), false);
      if (changed) {
        view.mock.value.config.revision = 9;
        view.mock.value.config.folder = '/mnt/preview-pool/remote-folder';
        view.mock.value.config.apps[0].name = 'Remote app name';
        view.mock.value.config.apps[0].args = ['--remote'];
        view.mock.value.config.runtimeDefaults.storageRoot = '/mnt/preview-pool/remote-root';
      }
      release(); release = null; await flush();
      assertDraft(); assert.equal(scenario.save(view.element).disabled, false);
    }
    globalThis.fetch = normalFetch;
    submit(field.closest('form')); await flush();
    const saved = view.mock.calls.find(call => call.action === 'save');
    assert.ok(saved);
    if (scenario.name === 'manual executable') {
      assert.equal(saved.expectedRevision, 9, 'New additions use the latest confirmed snapshot');
      assert.equal(view.mock.value.config.apps.at(-1).path, scenario.draft);
      assert.equal(view.mock.value.config.apps[0].name, 'Remote app name');
      assert.equal(view.mock.value.config.folder, '/mnt/preview-pool/remote-folder');
    } else {
      assert.equal(saved.expectedRevision, 2, 'Existing drafts retain their original CAS revision');
      assert.equal(view.mock.value.config.revision, 9, 'A conflict must not overwrite newer host settings');
      assert.equal(view.element.querySelector(scenario.selector).value, scenario.draft);
      assert.match(view.element.textContent, /Configuration changed elsewhere/);
    }
  } finally { if (release) release(); view.close(); polling.restore(); }
});

test('draft editability does not weaken initial load, stale status, mutation or request serialization guards', async () => {
  const calls = []; let manager; let release; let fail = false;
  const send = async payload => {
    calls.push(payload);
    await new Promise(resolve => { release = resolve; });
    if (fail) throw new Error('Offline');
    return runtimeFixture();
  };
  const element = document.createElement('main'); document.body.append(element);
  const app = createApp({ setup() { manager = useManager(send, 100000); return () => h('div'); } }); app.mount(element);
  try {
    assert.equal(manager.formDisabled.value, true); assert.equal(manager.disabled.value, true);
    release(); await flush();
    assert.equal(manager.formDisabled.value, false); assert.equal(manager.disabled.value, false);
    const poll = manager.refresh(false); await flush();
    assert.equal(manager.formDisabled.value, false); assert.equal(manager.disabled.value, true);
    assert.equal(await manager.mutate({ action: 'restart', id: 'test' }), false);
    assert.equal(await manager.refresh(false), false); assert.deepEqual(calls.map(call => call.action), ['status', 'status']);
    release(); await poll;
    const mutation = manager.mutate({ action: 'restart', id: 'test' }); await flush();
    assert.equal(manager.formDisabled.value, true); assert.equal(manager.disabled.value, true);
    assert.equal(await manager.refresh(false), false); assert.equal(await manager.mutate({ action: 'restart', id: 'test' }), false);
    assert.deepEqual(calls.map(call => call.action), ['status', 'status', 'restart']);
    release(); await mutation;
    assert.equal(manager.formDisabled.value, false); assert.equal(manager.disabled.value, false);
    fail = true; const outage = manager.refresh(false); await flush(); release(); await outage;
    assert.equal(manager.stale.value, true); assert.equal(manager.formDisabled.value, true); assert.equal(manager.disabled.value, true);
    fail = false; const recovery = manager.refresh(false); await flush();
    assert.equal(manager.formDisabled.value, true, 'Stale drafts stay disabled until status is confirmed');
    release(); await recovery;
    assert.equal(manager.formDisabled.value, false); assert.equal(manager.disabled.value, false);
  } finally { release?.(); app.unmount(); element.remove(); }
});
