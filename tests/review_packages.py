#!/usr/bin/env python3
"""Read/extract built Debian artifacts. Never install or run maintainer scripts."""
from hashlib import md5, sha256
import json
from pathlib import Path
import stat
import struct
import subprocess
import tempfile

project = Path(__file__).resolve().parent.parent
packages = sorted((project / 'artifacts').glob('*.deb'))
version = json.loads((project / 'page/package.json').read_text())['version']
assert len(packages) == 2, packages
checks = []
for package in packages:
    metadata = subprocess.check_output(['dpkg-deb', '-f', str(package)], text=True)
    assert 'Package: binary-manager-plugin\n' in metadata
    assert f'Version: {version}-1+mos-plugin\n' in metadata
    arch = 'arm64' if package.name.endswith('_arm64.deb') else 'amd64'
    assert f'Architecture: {arch}\n' in metadata
    data = package.read_bytes()
    for algorithm, digest in [('md5', md5(data).hexdigest()), ('sha256', sha256(data).hexdigest())]:
        assert Path(str(package) + '.' + algorithm).read_text().split() == [digest, package.name]
    listing = subprocess.check_output(['dpkg-deb', '-c', str(package)], text=True)
    assert all(' root/root ' in line for line in listing.splitlines())
    with tempfile.TemporaryDirectory(prefix='bm-package-review-') as directory:
        extracted = Path(directory)
        subprocess.run(['dpkg-deb', '-x', str(package), str(extracted / 'files')], check=True)
        subprocess.run(['dpkg-deb', '-e', str(package), str(extracted / 'control')], check=True)
        runtime = extracted / 'files/usr/bin/plugins/binary-manager'
        elf = runtime.read_bytes()
        assert elf[:6] == b'\x7fELF\x02\x01', elf[:6]
        machine = struct.unpack_from('<H', elf, 18)[0]
        assert machine == {'amd64': 62, 'arm64': 183}[arch], machine
        assert stat.S_IMODE(runtime.stat().st_mode) == 0o755
        program_headers = subprocess.check_output(['readelf', '-l', str(runtime)], text=True)
        assert 'INTERP' not in program_headers, 'Runtime unexpectedly needs a dynamic interpreter'
        dynamic = subprocess.check_output(['readelf', '-d', str(runtime)], text=True)
        assert 'There is no dynamic section' in dynamic
        web = extracted / 'files/var/www/mos-plugins/binary-manager'
        dist = project / 'page/dist/binary-manager'
        expected = sorted(path.relative_to(dist) for path in dist.rglob('*') if path.is_file())
        actual = sorted(path.relative_to(web) for path in web.rglob('*') if path.is_file())
        assert sorted(expected + [Path('icon.svg')]) == actual
        for relative in expected:
            assert (dist / relative).read_bytes() == (web / relative).read_bytes(), relative
        manifest = json.loads((web / 'manifest.json').read_text())
        assert manifest['name'] == 'binary-manager' and manifest['version'] == version
        assert manifest['icon'] == '/plugins/binary-manager/icon.svg'
        assert (web / 'icon.svg').read_bytes() == (project / 'hub/images/binary-managerIcon.svg').read_bytes()
        assert './Plugin' in (web / 'remoteEntry.js').read_text()
        for filename in ['functions', 'settings.json']:
            assert (extracted / 'files/usr/share/binary-manager' / filename).read_bytes() == (project / filename).read_bytes()
        docs = extracted / 'files/usr/share/doc/binary-manager-plugin'
        assert (docs / 'LICENSE').read_bytes() == (project / 'LICENSE').read_bytes()
        assert (docs / 'THIRD_PARTY_NOTICES.txt').read_bytes() == (project / 'docs/THIRD_PARTY_NOTICES.txt').read_bytes()
        control = extracted / 'control'
        assert sorted(path.name for path in control.iterdir()) == ['control', 'prerm']
        prerm = (control / 'prerm').read_text()
        assert 'upgrade) /usr/bin/plugins/binary-manager shutdown ;;' in prerm
        assert 'remove|deconfigure) /usr/bin/plugins/binary-manager shutdown --clear ;;' in prerm
        assert 'pkill' not in prerm and 'killall' not in prerm
        subprocess.run(['sh', '-n', str(control / 'prerm')], check=True)
        if arch == 'amd64':
            subprocess.run(['python3', str(project / 'tests/review_child_launch.py'), str(runtime)], check=True)
        checks.append({'architecture': arch, 'ELF_machine': machine, 'static': True, 'web_files_verified': len(actual), 'checksums': 'MD5 + SHA-256 match', 'maintainer_scripts': 'prerm only; inspected, never executed'})
print(json.dumps({'ok': True, 'packages': checks}, indent=2))
