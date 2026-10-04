#!/usr/bin/env python3
"""Create an offline source ZIP. Does not install, run apps, or publish."""
from pathlib import Path
import json
import re
import zipfile

root = Path(__file__).resolve().parents[1]
version = json.loads((root / 'page/package.json').read_text())['version']
if not re.fullmatch(r'\d+\.\d+\.\d+', version):
    raise SystemExit('Version must be numeric x.y.z')
out = root / 'artifacts' / f'mos-binary-manager-{version}-source.zip'
out.parent.mkdir(exist_ok=True)
excluded = {'node_modules', 'dist', 'preview-dist', 'artifacts', '.git', '__pycache__', '.cache'}
allowed_roots = {'backend', 'page', 'scripts', 'docs', 'tests', 'checksums', '.github', 'hub'}
allowed_files = {'README.md', 'LICENSE', 'functions', 'settings.json', '.gitignore'}
files = []
for path in sorted(root.rglob('*')):
    rel = path.relative_to(root)
    if any(part in excluded for part in rel.parts) or str(rel) == 'tests/review_findings.md':
        continue
    if path.is_symlink():
        raise SystemExit(f'Refusing symlink in source: {rel}')
    if not path.is_file() or (rel.parts[0] not in allowed_roots and str(rel) not in allowed_files):
        continue
    if path.stat().st_size > 2_000_000:
        raise SystemExit(f'Unexpectedly large source file: {rel}')
    files.append((path, rel))
with zipfile.ZipFile(out, 'x', zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
    for path, rel in files:
        archive.write(path, f'mos-binary-manager/{rel.as_posix()}')
with zipfile.ZipFile(out) as archive:
    bad = archive.testzip()
    if bad:
        raise SystemExit(f'ZIP integrity check failed: {bad}')
print(f'{out} ({len(files)} source files)')
