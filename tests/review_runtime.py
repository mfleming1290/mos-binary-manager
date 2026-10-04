#!/usr/bin/env python3
"""Independent temp-root CLI safety checks; supply a locally built helper path.

No MOS installation, real host paths or unknown executables are used. The only
programs launched are this generated Python fixture and a temporary copy of the
system's sleep binary. Every helper invocation carries BINARY_MANAGER_ROOT.
"""
import base64
from concurrent.futures import ThreadPoolExecutor
import copy
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time


def main():
    if len(sys.argv) != 2:
        raise SystemExit('Usage: python3 tests/review_runtime.py /absolute/path/to/built/binary-manager')
    binary = str(Path(sys.argv[1]).resolve(strict=True))
    with tempfile.TemporaryDirectory(prefix='bm-review-') as directory:
        root = Path(directory)
        env = dict(os.environ, BINARY_MANAGER_ROOT=str(root))
        unmanaged = None
        checks = []

        def cli(*args, expected=0, override_env=None):
            completed = subprocess.run([binary, *args], env=override_env or env, capture_output=True, text=True, timeout=25)
            assert completed.returncode == expected, (args[0], completed.returncode, completed.stdout, completed.stderr)
            return json.loads(completed.stdout)

        def request(payload):
            token = base64.urlsafe_b64encode(json.dumps(payload, ensure_ascii=False).encode()).decode().rstrip('=')
            return cli('request', token)

        def status():
            value = request({'action': 'status'})
            assert value['ok'], value
            return value

        def until(predicate, seconds=8):
            deadline = time.monotonic() + seconds
            last = None
            while time.monotonic() < deadline:
                last = status()
                if predicate(last):
                    return last
                time.sleep(0.08)
            raise AssertionError(('timed out', last))

        def process(value, app_id):
            return next(app for app in value['apps'] if app['id'] == app_id)

        try:
            assert cli('ensure')['ok']
            initial = status()
            assert initial['config']['folder'] == '' and initial['config']['apps'] == []
            checks.append('default state contains no apps or implicit execution')

            programs = root / 'programs $[] é'
            programs.mkdir()
            args_fixture = programs / 'print argv (known fixture)'
            args_fixture.write_text('#!/usr/bin/python3\nimport json, os, sys\nprint(json.dumps({"argv": sys.argv[1:], "cwd": os.getcwd()}, ensure_ascii=False), flush=True)\n')
            args_fixture.chmod(0o700)
            sleeper = programs / 'same-name sleeper'
            shutil.copyfile('/bin/sleep', sleeper)
            sleeper.chmod(0o700)
            symlink = programs / 'symlink must not be accepted'
            symlink.symlink_to(sleeper)
            arguments = ['a b', '"quotes"', "'single'", '$HOME', '$(touch should-not-exist)', '\\', '', 'line\nbreak', '日本語🙂']
            config = initial['config']
            config.update({'folder': str(programs), 'futureField': {'preserve': True}, 'apps': [
                {'id': 'argv', 'path': str(args_fixture), 'name': 'ARGV', 'args': arguments, 'workdir': '', 'autostart': True, 'futureAppField': 12},
                {'id': 'sleeper', 'path': str(sleeper), 'name': 'Sleeper', 'args': ['60'], 'workdir': '', 'autostart': False},
            ]})
            saved = request({'action': 'save', 'expectedRevision': 0, 'config': config})
            assert saved['ok'], saved
            assert all(not p['running'] and not p['desired'] for p in saved['apps'])
            assert str(symlink) not in [entry['path'] for entry in saved['discovered']]
            checks.append('add/save never starts apps; executable symlinks are excluded from discovery')

            rejected = copy.deepcopy(saved['config'])
            rejected['apps'].append({'id': 'link', 'path': str(symlink), 'name': 'Link', 'args': [], 'workdir': '', 'autostart': False})
            assert request({'action': 'save', 'expectedRevision': 1, 'config': rejected})['ok'] is False
            assert status()['config']['revision'] == 1
            checks.append('symlink executable save fails without committing config')

            assert request({'action': 'toggle', 'id': 'argv', 'enabled': True})['ok']
            stopped = until(lambda value: process(value, 'argv')['state'] == 'exited')
            assert process(stopped, 'argv')['lastExit']['code'] == 0
            assert process(stopped, 'argv')['desired'] is False
            output = request({'action': 'logs', 'id': 'argv'})
            assert output['ok']
            lines = [json.loads(line) for line in output['lines'].splitlines()]
            assert lines == [{'argv': arguments, 'cwd': str(programs)}], lines
            checks.append('literal quotes, metacharacters, Unicode, empty/newline argv and default cwd survive execution')

            assert cli('boot')['ok']
            until(lambda value: process(value, 'argv')['state'] == 'exited' and process(value, 'argv')['desired'] is False)
            after_boot = request({'action': 'logs', 'id': 'argv'})['lines']
            assert len(after_boot.splitlines()) == 2, after_boot
            assert cli('boot')['ok']
            time.sleep(1.2)
            assert request({'action': 'logs', 'id': 'argv'})['lines'] == after_boot
            assert process(status(), 'argv')['restarts'] == 0
            checks.append('clean exit stops; one boot application; repeated same-boot hook does not relaunch')

            unmanaged = subprocess.Popen([str(sleeper), '60'], env=env)
            started = request({'action': 'toggle', 'id': 'sleeper', 'enabled': True})
            assert started['ok']
            live = until(lambda value: process(value, 'sleeper')['running'])
            assert process(live, 'sleeper')['pid'] != unmanaged.pid
            assert request({'action': 'toggle', 'id': 'sleeper', 'enabled': False})['ok']
            assert process(status(), 'sleeper')['running'] is False
            assert unmanaged.poll() is None
            checks.append('stopping managed process preserves unmanaged process running the exact same executable')

            base = status()['config']
            def competing_save(name):
                changed = copy.deepcopy(base)
                changed['apps'][0]['name'] = name
                return request({'action': 'save', 'expectedRevision': base['revision'], 'config': changed})
            with ThreadPoolExecutor(max_workers=2) as executor:
                responses = list(executor.map(competing_save, ['First writer', 'Second writer']))
            assert sum(response['ok'] for response in responses) == 1, responses
            assert next(response for response in responses if not response['ok'])['code'] == 'REVISION_CONFLICT'
            current = status()['config']
            assert current['revision'] == base['revision'] + 1
            assert current['futureField'] == {'preserve': True}
            assert current['apps'][0]['futureAppField'] == 12
            checks.append('concurrent writers produce one commit and one revision conflict; unknown fields persist')

            invalid_env = dict(env, BINARY_MANAGER_ROOT='/')
            for action in ['ensure', 'boot', 'shutdown']:
                assert cli(action, expected=1, override_env=invalid_env)['ok'] is False
            checks.append('lifecycle failures exit nonzero for shell/package hooks')

            assert cli('shutdown', '--clear')['ok']
            assert unmanaged.poll() is None
            checks.append('shutdown waits for completion and leaves unrelated process intact')
            print(json.dumps({'ok': True, 'checks': checks}, indent=2))
        finally:
            try:
                cli('shutdown', '--clear')
            finally:
                if unmanaged is not None:
                    unmanaged.terminate()
                    unmanaged.wait(timeout=5)


if __name__ == '__main__':
    main()
