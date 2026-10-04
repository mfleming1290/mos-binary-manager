// Independent regression checks. These never contact MOS or run user binaries.
import test from 'node:test';
import assert from 'node:assert/strict';
import { encodeRequest, decodeQuery, request } from '../page/src/api.js';
import { argumentLines, editApp, newApp, validateConfig, availableDiscoveries } from '../page/src/model.js';

const config = () => ({ schemaVersion: 1, revision: 0, folder: '', apps: [], future: { preserve: true } });

test('transport keeps hostile path/argv bytes out of MOS shell text', () => {
  const unusual = ['plain', 'two words', '"double"', "'single'", '\\', '$HOME', '$(touch nope)', '`touch nope`', '; & | ( ) { } [ ] < >', '\n\r\t', '日本語/🙂', '', '\u0000'];
  for (const text of unusual) {
    const value = { action: 'save', path: `/fixture/${text}`, config: { args: unusual, name: text } };
    const token = encodeRequest(value);
    assert.match(token, /^[A-Za-z0-9_-]+$/);
    assert.doesNotMatch(token, /[;&|`$(){}[\]<>\r\n"'\\\s=]/);
    assert.deepEqual(JSON.parse(Buffer.from(token, 'base64url').toString('utf8')), value);
  }
});

test('large Unicode payload round trips without lossy UTF-16 conversion', () => {
  const value = { action: 'save', config: { args: ['🙂日本語'.repeat(2000)] } };
  assert.deepEqual(JSON.parse(Buffer.from(encodeRequest(value), 'base64url').toString('utf8')), value);
});

test('literal arguments retain quotes, whitespace and empty interior/trailing arguments', () => {
  assert.deepEqual(argumentLines('--name\n  a b  \n"quoted"\n\n$HOME\n'), ['--name', '  a b  ', '"quoted"', '', '$HOME', '']);
  assert.deepEqual(argumentLines(''), []);
  assert.deepEqual(argumentLines('a\r\nb'), ['a', 'b']);
});

test('empty configuration stays empty and unknown settings survive edits', () => {
  const current = config();
  assert.deepEqual(validateConfig(current), current);
  const app = { ...newApp('/tmp/known fixture', 'fixture'), futureAppKey: { keep: 'yes' } };
  current.apps.push(app);
  const changed = editApp(current, 'fixture', { name: 'New label' });
  assert.equal(changed.folder, '');
  assert.equal(changed.apps[0].autostart, false);
  assert.deepEqual(changed.apps[0].args, []);
  assert.equal(changed.apps[0].workdir, '');
  assert.deepEqual(changed.future, { preserve: true });
  assert.deepEqual(changed.apps[0].futureAppKey, { keep: 'yes' });
  assert.equal(current.apps[0].name, 'known fixture');
});

test('discovery never mutates or auto-adds listed executables', () => {
  const current = config();
  current.apps.push(newApp('/tmp/one', 'one'));
  const status = { config: current, discovered: [{ name: 'one', path: '/tmp/one' }, { name: 'two', path: '/tmp/two' }] };
  assert.deepEqual(availableDiscoveries(status), [{ name: 'two', path: '/tmp/two' }]);
  assert.equal(current.apps.length, 1);
  assert.equal(current.apps[0].autostart, false);
});

test('null and false responses cannot masquerade as successful host outcomes', () => {
  for (const output of [null, [], {}, { ok: 'true' }, 'not JSON']) {
    assert.throws(() => decodeQuery({ success: true, exit_code: 0, output }));
  }
  assert.throws(() => decodeQuery({ success: true, exit_code: 0, output: { ok: false, code: 'STOP_FAILED', error: 'Stop could not be verified' } }), error => error.code === 'STOP_FAILED');
  assert.throws(() => decodeQuery({ success: false, exit_code: 0, output: { ok: true } }), error => error.code === 'QUERY_FAILED');
  assert.throws(() => decodeQuery({ success: true, exit_code: 0, timed_out: true, output: { ok: true } }), error => error.code === 'TIMEOUT');
});

test('request crosses MOS query only as a fixed helper and safe token', async () => {
  const oldFetch = globalThis.fetch;
  const oldStorage = globalThis.localStorage;
  const value = { action: 'toggle', id: 'fixture', enabled: false, arbitrary: '"\\;$()[]\n日本語' };
  try {
    globalThis.localStorage = { getItem: key => key === 'authToken' ? 'review-token' : null };
    globalThis.fetch = async (url, options) => {
      assert.equal(url, '/api/v1/mos/plugins/query');
      assert.equal(options.method, 'POST');
      assert.equal(options.headers.Authorization, 'Bearer review-token');
      const body = JSON.parse(options.body);
      assert.equal(body.command, 'binary-manager');
      assert.equal(body.args.length, 2);
      assert.equal(body.args[0], 'request');
      assert.match(body.args[1], /^[A-Za-z0-9_-]+$/);
      assert.deepEqual(JSON.parse(Buffer.from(body.args[1], 'base64url').toString('utf8')), value);
      assert.equal(body.parse_json, true);
      return { ok: true, json: async () => ({ success: true, exit_code: 0, output: { ok: true } }) };
    };
    assert.deepEqual(await request(value), { ok: true });
  } finally {
    globalThis.fetch = oldFetch;
    globalThis.localStorage = oldStorage;
  }
});
