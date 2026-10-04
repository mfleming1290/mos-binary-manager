// Offline build only. Never installs on MOS or publishes anything.
import { cpSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync, linkSync, chmodSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
import { execFileSync } from 'node:child_process';
import config from '../page/plugin.config.js';
const root = fileURLToPath(new URL('../', import.meta.url));
const dist = join(root, 'page', 'dist', config.name);
const out = join(root, 'artifacts');
const manifest = JSON.parse(readFileSync(join(dist, 'manifest.json'), 'utf8'));
const arches = (process.env.ARCHES || 'amd64 arm64').split(/\s+/).filter(Boolean);
const go = process.env.GO || 'go';
if (!/^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(config.name)) throw new Error('Invalid plugin name');
if (manifest.name !== config.name || manifest.version !== config.version) throw new Error('Config and built manifest differ; rebuild the frontend');
if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(manifest.version)) throw new Error('Version must be numeric x.y.z');
if (!readFileSync(join(dist, 'remoteEntry.js'), 'utf8').includes('./Plugin')) throw new Error('remoteEntry.js must expose ./Plugin');
if (!arches.length || arches.some(a => !['amd64', 'arm64'].includes(a)) || new Set(arches).size !== arches.length) throw new Error('ARCHES must contain amd64 and/or arm64');
function noLinks(path) {
  if (lstatSync(path).isSymbolicLink()) throw new Error(`Symlinks are not packaged: ${path}`);
  if (lstatSync(path).isDirectory()) for (const entry of readdirSync(path)) noLinks(join(path, entry));
}
noLinks(dist);
if (existsSync(out) && lstatSync(out).isSymbolicLink()) throw new Error('artifacts must not be a symlink');
mkdirSync(out, { recursive: true });
for (const arch of arches) {
  const filename = `${config.name}_${manifest.version}-1+mos-plugin_${arch}.deb`;
  const deb = join(out, filename);
  if ([deb, `${deb}.md5`, `${deb}.sha256`].some(existsSync)) throw new Error(`Package already exists: ${filename}; move old build artifacts explicitly`);
  const work = mkdtempSync(join(out, '.build-'));
  try {
    const tree = join(work, 'package');
    for (const dir of ['DEBIAN', 'var/www/mos-plugins', 'usr/bin/plugins', 'usr/share/doc/binary-manager-plugin', 'usr/share/binary-manager']) mkdirSync(join(tree, dir), { recursive: true });
    cpSync(dist, join(tree, 'var/www/mos-plugins', config.name), { recursive: true });
    cpSync(join(root, 'hub/images/binary-managerIcon.svg'), join(tree, 'var/www/mos-plugins', config.name, 'icon.svg'));
    execFileSync(go, ['build', '-buildvcs=false', '-trimpath', '-ldflags=-s -w', '-o', join(tree, 'usr/bin/plugins/binary-manager'), '.'], {
      cwd: join(root, 'backend'), stdio: 'inherit', env: { ...process.env, CGO_ENABLED: '0', GOOS: 'linux', GOARCH: arch, GOTOOLCHAIN: 'local' },
    });
    chmodSync(join(tree, 'usr/bin/plugins/binary-manager'), 0o755);
    for (const file of ['functions', 'settings.json']) cpSync(join(root, file), join(tree, 'usr/share/binary-manager', file));
    for (const [source, target] of [['README.md', 'README.md'], ['LICENSE', 'LICENSE'], ['docs/THIRD_PARTY_NOTICES.txt', 'THIRD_PARTY_NOTICES.txt']]) {
      cpSync(join(root, source), join(tree, 'usr/share/doc/binary-manager-plugin', target));
    }
    mkdirSync(join(tree, 'usr/share/doc/binary-manager-plugin/docs'), { recursive: true });
    cpSync(join(root, 'docs/INSTANCE-SETTINGS.md'), join(tree, 'usr/share/doc/binary-manager-plugin/docs/INSTANCE-SETTINGS.md'));
    writeFileSync(join(tree, 'DEBIAN/control'), [
      'Package: binary-manager-plugin', `Version: ${manifest.version}-1+mos-plugin`, `Architecture: ${arch}`,
      `Maintainer: ${config.author}`, 'Section: admin', 'Priority: optional',
      'Description: Foreground binary supervision and boot startup for MOS NAS',
      ' A MOS plugin with explicit process controls, bounded logs and restart-on-failure.', '',
    ].join('\n'));
    // Stop owned processes even if MOS selects the wrong uninstall hook.
    // Refuse package removal when owned-process shutdown reports failure.
    writeFileSync(join(tree, 'DEBIAN/prerm'), `#!/bin/sh\nset -eu\nif [ -x /usr/bin/plugins/binary-manager ]; then\n  case "$1" in\n    upgrade) /usr/bin/plugins/binary-manager shutdown ;;\n    remove|deconfigure) /usr/bin/plugins/binary-manager shutdown --clear ;;\n  esac\nfi\n`);
    chmodSync(join(tree, 'DEBIAN/prerm'), 0o755);
    const built = join(work, filename);
    execFileSync('dpkg-deb', ['--root-owner-group', '--build', tree, built], { stdio: 'inherit' });
    linkSync(built, deb);
    for (const algo of ['md5', 'sha256']) writeFileSync(`${deb}.${algo}`, `${createHash(algo).update(readFileSync(deb)).digest('hex')}  ${filename}\n`, { flag: 'wx' });
    console.log(`Package: ${deb}`);
  } finally {
    rmSync(work, { recursive: true, force: true }); // Only this invocation's newly allocated staging directory.
  }
}
