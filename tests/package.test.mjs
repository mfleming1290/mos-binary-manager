import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
const root = fileURLToPath(new URL('../', import.meta.url));
test('source hooks are inert when sourced and expose only lifecycle definitions', () => {
  assert.equal(execFileSync('bash', ['-c', '. "$1"; declare -F', 'test', join(root, 'functions')], { encoding: 'utf8' }).trim(),
    'declare -f install\ndeclare -f mos_start_after_services\ndeclare -f plugin_update\ndeclare -f uninstall');
});
test('default config never enables applications', () => {
  assert.deepEqual(JSON.parse(readFileSync(join(root, 'settings.json'))), { schemaVersion: 1, revision: 0, folder: '', apps: [] });
});
test('manual installer documents scope and rejects arguments without host mutation', () => {
  assert.match(execFileSync('bash', [join(root, 'scripts/install-local.sh'), '--help'], {encoding:'utf8'}), /First install only/);
});
test('built packages contain UI, native helper and owned-process removal hook', () => {
  const files = existsSync(join(root, 'artifacts')) ? readdirSync(join(root, 'artifacts')).filter(x => x.endsWith('.deb')) : [];
  assert.ok(files.length, 'Build packages before running this check');
  for (const f of files) {
    const file = join(root, 'artifacts', f);
    const metadata = execFileSync('dpkg-deb', ['-f', file], {encoding:'utf8'});
    assert.match(metadata, /Package: binary-manager-plugin/);
    assert.match(metadata, /Architecture: (amd64|arm64)/);
    const listing = execFileSync('dpkg-deb', ['-c', file], {encoding:'utf8'});
    for (const name of ['usr/bin/plugins/binary-manager', 'manifest.json', 'remoteEntry.js', 'usr/share/binary-manager/functions']) assert.ok(listing.includes(name), name);
    assert.ok(existsSync(`${file}.sha256`));
    assert.ok(existsSync(`${file}.md5`));
  }
});
