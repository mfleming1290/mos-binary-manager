import test, { after } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { join } from 'node:path';
import { parse, compileScript } from '@vue/compiler-sfc';
import { JSDOM } from 'jsdom';
import { createMock, fixture } from '../preview/mock.js';

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
