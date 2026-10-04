import test from 'node:test';
import assert from 'node:assert/strict';
import { api } from './api.js';

test('uses current bearer token and JSON body without logging credentials', async () => {
  globalThis.localStorage = { getItem: () => 'test-token' };
  globalThis.fetch = async (url, options) => {
    assert.equal(url, '/api/v1/mos/plugins/settings/demo');
    assert.equal(options.headers.Authorization, 'Bearer test-token');
    assert.deepEqual(JSON.parse(options.body), { enabled: true });
    return { ok: true, json: async () => ({ enabled: true }) };
  };
  assert.deepEqual(await api('/settings/demo', { method: 'POST', body: { enabled: true } }), { enabled: true });
});
test('missing sign-in stops before fetch', async () => {
  globalThis.localStorage = { getItem: () => null };
  globalThis.fetch = async () => { throw new Error('Must not fetch'); };
  await assert.rejects(api('/settings/demo'), /Sign in to MOS/);
});
test('HTTP error is reported and not treated as saved', async () => {
  globalThis.localStorage = { getItem: () => 'test-token' };
  globalThis.fetch = async () => ({ ok: false, status: 403, json: async () => ({ error: 'Forbidden' }) });
  await assert.rejects(api('/settings/demo'), /Forbidden/);
});
test('invalid error body still reports HTTP status', async () => {
  globalThis.fetch = async () => ({ ok: false, status: 500, json: async () => { throw new Error('Not JSON'); } });
  await assert.rejects(api('/settings/demo'), /500/);
});

import { encodeRequest, decodeQuery, request } from './api.js';
test('query payload is a single shell-safe base64url string with exact UTF-8 roundtrip', () => {
  const payload = { action: 'save', config: { path: '/mnt/用户/a b";$(touch x)', args: ['$(whoami)', 'a\nb', '', '\"'] } };
  const encoded = encodeRequest(payload);
  assert.match(encoded, /^[A-Za-z0-9_-]+$/);
  assert.deepEqual(JSON.parse(Buffer.from(encoded, 'base64url').toString('utf8')), payload);
  assert.throws(() => encodeRequest({ value: 'x'.repeat(65536) }), /64 KiB/);
});
test('query rejects timeout, command failures, unparsed output and semantic errors', () => {
  const good = { success: true, exit_code: 0, timed_out: false, output: { ok: true } };
  assert.deepEqual(decodeQuery(good), { ok: true });
  for (const bad of [null, {}, { ...good, timed_out: true }, { ...good, success: false }, { ...good, exit_code: 1 }, { ...good, output: '{"ok":true}' }, { ...good, output: [] }]) assert.throws(() => decodeQuery(bad));
  assert.throws(() => decodeQuery({ ...good, output: { ok: false, code: 'REVISION_CONFLICT', error: 'Conflict' } }), error => error.code === 'REVISION_CONFLICT');
});
test('all application requests use the fixed narrow query command and preserve abort support', async () => {
  globalThis.localStorage = { getItem: () => 'test-token' };
  globalThis.fetch = async (url, options) => {
    assert.equal(url, '/api/v1/mos/plugins/query');
    const body = JSON.parse(options.body);
    assert.equal(body.command, 'binary-manager');
    assert.deepEqual(JSON.parse(Buffer.from(body.args[1], 'base64url')), { action: 'status' });
    assert.equal(body.args[0], 'request');
    assert.equal(body.parse_json, true);
    assert.ok(options.signal);
    return { ok: true, json: async () => ({ success: true, output: { ok: true }, exit_code: 0, timed_out: false }) };
  };
  assert.deepEqual(await request({ action: 'status' }), { ok: true });
});
