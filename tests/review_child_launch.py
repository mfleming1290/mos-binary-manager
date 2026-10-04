#!/usr/bin/env python3
"""Exercise the private launch gate without a socket or MOS host."""
import base64
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

if len(sys.argv) != 2:
    raise SystemExit('Usage: python3 tests/review_child_launch.py /absolute/path/to/built/binary-manager')
helper = str(Path(sys.argv[1]).resolve(strict=True))
with tempfile.TemporaryDirectory(prefix='bm-gate-review-') as directory:
    root = Path(directory)
    fixture = root / 'known fixture $[] é'
    fixture.write_text('#!/usr/bin/python3\nimport json, os, sys\nprint(json.dumps({"argv":sys.argv[1:],"cwd":os.getcwd()}, ensure_ascii=False), flush=True)\n')
    fixture.chmod(0o700)
    argv = ['a b', '"quotes"', "'single'", '日本語🙂', '', '$HOME', '$(touch DO_NOT_CREATE)', 'line\nbreak']
    payload = {'id': 'fixture', 'path': str(fixture), 'name': 'Fixture', 'args': argv, 'workdir': '', 'autostart': False}
    encoded = base64.urlsafe_b64encode(json.dumps(payload, ensure_ascii=False).encode()).decode().rstrip('=')
    for authorization in [b'', b'\x00', b'\x01']:
        read_fd, write_fd = os.pipe()
        # A standalone Python invocation has only stdin/out/err; reject rather
        # than overwrite another descriptor if the host changed that contract.
        assert read_fd == 3, f'Unexpected descriptor {read_fd}'
        child = subprocess.Popen([helper, '__child', encoded], pass_fds=(read_fd,), stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=dict(os.environ, BINARY_MANAGER_ROOT=str(root)))
        os.close(read_fd)
        if authorization:
            os.write(write_fd, authorization)
        os.close(write_fd)
        stdout, stderr = child.communicate(timeout=5)
        if authorization != b'\x01':
            assert child.returncode == 126 and stdout == b'', (child.returncode, stdout, stderr)
            assert b'did not authorize launch' in stderr
        else:
            assert child.returncode == 0, (child.returncode, stderr)
            assert json.loads(stdout) == {'argv': argv, 'cwd': str(root)}
    assert not (root / 'DO_NOT_CREATE').exists()
    print('PASS: EOF and invalid launch gates execute nothing; valid gate preserves literal argv and cwd; no shell expansion')
