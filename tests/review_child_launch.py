#!/usr/bin/env python3
"""Exercise the private launch gate/JSON pipe without a socket or MOS host."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

if len(sys.argv) != 2:
    raise SystemExit('Usage: python3 tests/review_child_launch.py /absolute/path/to/built/binary-manager')
helper = str(Path(sys.argv[1]).resolve(strict=True))
with tempfile.TemporaryDirectory(prefix='bm-gate-review-') as directory:
    root = Path(directory)
    fixture = root / 'known fixture $[] é'
    fixture.write_text('#!/usr/bin/python3\nimport json, os, sys\nprint(json.dumps({"argv":sys.argv[1:],"cwd":os.getcwd(),"privateEnvPresent":os.environ.get("BM_TEST_SECRET") == "private-pipe-marker"}, ensure_ascii=False), flush=True)\n')
    fixture.chmod(0o700)
    argv = ['a b', '"quotes"', "'single'", '日本語🙂', '', '$HOME', '$(touch DO_NOT_CREATE)', 'line\nbreak']
    env = [f'{key}={value}' for key, value in os.environ.items() if not key.startswith('BINARY_MANAGER_')]
    env.append('BM_TEST_SECRET=private-pipe-marker')
    payload = {'path': str(fixture), 'args': argv, 'workdir': '', 'env': env}
    encoded = json.dumps(payload, ensure_ascii=False).encode()
    for authorization in [b'', b'\x00', b'\x01']:
        read_fd, write_fd = os.pipe()
        assert read_fd == 3, f'Unexpected descriptor {read_fd}'
        child = subprocess.Popen([helper, '__child'], pass_fds=(read_fd,), stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=dict(os.environ))
        os.close(read_fd)
        command = b''
        deadline = time.monotonic() + 2
        while not command and time.monotonic() < deadline:
            command = Path(f'/proc/{child.pid}/cmdline').read_bytes()
            if not command:
                time.sleep(0.01)
        assert command.split(b'\0')[:2] == [os.fsencode(helper), b'__child'], command
        assert b'private-pipe-marker' not in command and os.fsencode(fixture) not in command
        if authorization:
            os.write(write_fd, authorization)
            if authorization == b'\x01':
                os.write(write_fd, encoded)
        os.close(write_fd)
        stdout, stderr = child.communicate(timeout=5)
        assert b'private-pipe-marker' not in stdout + stderr
        if authorization != b'\x01':
            assert child.returncode == 126 and stdout == b'', (child.returncode, stdout, stderr)
            assert b'did not authorize launch' in stderr
        else:
            assert child.returncode == 0, (child.returncode, stderr)
            assert json.loads(stdout) == {'argv': argv, 'cwd': str(root), 'privateEnvPresent': True}
    # Invalid private data may contain a secret; diagnostics must not echo it.
    read_fd, write_fd = os.pipe()
    assert read_fd == 3
    child = subprocess.Popen([helper, '__child'], pass_fds=(read_fd,), stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    os.close(read_fd)
    os.write(write_fd, b'\x01{"private-pipe-marker":invalid JSON}')
    os.close(write_fd)
    stdout, stderr = child.communicate(timeout=5)
    assert child.returncode == 126 and stdout == b'' and b'private-pipe-marker' not in stderr
    assert b'invalid launch data' in stderr
    assert not (root / 'DO_NOT_CREATE').exists()
    print('PASS: invalid/EOF gates execute nothing; private FD delivers literal argv/env; helper argv/errors exclude secret marker; no shell expansion')
