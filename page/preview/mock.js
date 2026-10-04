export function fixture() {
  return {
    ok: true,
    config: { schemaVersion: 1, revision: 2, folder: '/mnt/user/applications', preserved: { future: true }, apps: [
      { id: '11111111-1111-4111-8111-111111111111', name: 'Syncthing', path: '/mnt/user/applications/syncthing', args: ['serve', '--no-browser'], workdir: '', autostart: true, futureAppKey: 'preserved' },
      { id: '22222222-2222-4222-8222-222222222222', name: 'Restic backup', path: '/opt/restic', args: [], workdir: '', autostart: false },
    ] },
    apps: [
      { id: '11111111-1111-4111-8111-111111111111', desired: true, running: true, pid: 4821, state: 'running', restarts: 0, lastExit: null, error: '' },
      { id: '22222222-2222-4222-8222-222222222222', desired: false, running: false, pid: null, state: 'exited', restarts: 0, lastExit: { code: 0, signal: null, at: 1759409220 }, error: '' },
    ],
    discovered: [{ name: 'syncthing', path: '/mnt/user/applications/syncthing' }, { name: 'filebrowser', path: '/mnt/user/applications/filebrowser' }, { name: 'rclone', path: '/mnt/user/applications/rclone' }],
    folderError: '', supervisor: { pid: 2031, mode: 'boot', bootApplied: true },
  };
}
const copy = value => JSON.parse(JSON.stringify(value));
export function createMock(initial = fixture()) {
  const state = { value: copy(initial), calls: [], failStatus: false, failNext: false, conflictNext: false, delay: 40 };
  state.handle = payload => {
    state.calls.push(copy(payload));
    if (state.failStatus && payload.action === 'status') throw new Error('Preview: host is unavailable.');
    if (payload.action !== 'status' && state.failNext) { state.failNext = false; throw new Error('Preview: connection lost during request.'); }
    if (['save', 'remove'].includes(payload.action)) {
      if (state.conflictNext) { state.conflictNext = false; state.value.config.revision++; }
      if (payload.expectedRevision !== state.value.config.revision) return { ok: false, code: 'REVISION_CONFLICT', error: 'Configuration changed elsewhere.' };
    }
    if (payload.action === 'save') {
      state.value.config = { ...copy(payload.config), revision: state.value.config.revision + 1 };
      state.value.apps = state.value.config.apps.map(app => state.value.apps.find(item => item.id === app.id) || { id: app.id, desired: false, running: false, pid: null, state: 'stopped', restarts: 0, lastExit: null, error: '' });
    }
    if (['toggle', 'restart'].includes(payload.action)) {
      const app = state.value.apps.find(item => item.id === payload.id);
      if (!app) return { ok: false, code: 'NOT_FOUND', error: 'App not found.' };
      const enabled = payload.action === 'restart' || payload.enabled;
      Object.assign(app, { desired: enabled, running: enabled, state: enabled ? 'running' : 'stopped', pid: enabled ? 5200 : null });
    }
    if (payload.action === 'remove') {
      state.value.config.apps = state.value.config.apps.filter(app => app.id !== payload.id);
      state.value.apps = state.value.apps.filter(app => app.id !== payload.id);
      state.value.config.revision++;
    }
    if (payload.action === 'logs') return { ok: true, lines: '[01:02:31] Starting foreground process\n[01:02:31] Listening on 127.0.0.1\n[01:03:02] Ready\n' };
    if (payload.action === 'browse') {
      const paths = { '/': [{ name: 'mnt', path: '/mnt', type: 'directory' }, { name: 'opt', path: '/opt', type: 'directory' }], '/mnt': [{ name: 'user', path: '/mnt/user', type: 'directory' }], '/mnt/user': [{ name: 'applications', path: '/mnt/user/applications', type: 'directory' }], '/mnt/user/applications': state.value.discovered.map(app => ({ ...app, type: 'executable' })), '/opt': [{ name: 'restic', path: '/opt/restic', type: 'executable' }, { name: 'my server', path: '/opt/my server', type: 'executable' }] };
      if (!paths[payload.path]) return { ok: false, code: 'INVALID_PATH', error: 'Folder not found in the local preview fixture.' };
      return { ok: true, path: payload.path, parent: payload.path === '/' ? null : payload.path.slice(0, payload.path.lastIndexOf('/')) || '/', entries: paths[payload.path] };
    }
    return copy(state.value);
  };
  state.fetch = async (url, options) => {
    if (url !== '/api/v1/mos/plugins/query') throw new Error('Unexpected preview request');
    const body = JSON.parse(options.body);
    if (body.command !== 'binary-manager' || body.args?.[0] !== 'request' || !/^[A-Za-z0-9_-]+$/.test(body.args[1])) throw new Error('Invalid query transport');
    const payload = JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(body.args[1].replaceAll('-', '+').replaceAll('_', '/')), c => c.charCodeAt(0))));
    if (options.signal?.aborted) throw new DOMException('Aborted', 'AbortError');
    if (state.delay) await new Promise((resolve, reject) => {
      const timer = setTimeout(resolve, state.delay);
      options.signal?.addEventListener('abort', () => { clearTimeout(timer); reject(new DOMException('Aborted', 'AbortError')); }, { once: true });
    });
    const result = state.handle(payload);
    return { ok: true, json: async () => ({ success: true, output: result, timed_out: false, exit_code: 0, duration_ms: state.delay }) };
  };
  return state;
}

// Simulated paths only; no mounted pool is inferred for a real NAS.
export function runtimeFixture() {
  const data = fixture();
  data.config.runtimeDefaults = { storageRoot: '/mnt/preview-pool/instances', pathDirs: ['/mnt/preview-pool/tools'], env: { SHARED: 'yes', REMOVE: null }, futureDefaults: { keep: true } };
  data.config.apps[0].runtime = { useDefaults: false, homeMode: 'custom', home: '/mnt/preview-pool/original', pathDirs: ['/opt/bin'], env: { MODE: 'one', EMPTY: '' }, envFile: '/mnt/preview-pool/private/original.env', xdgConfigHome: '/mnt/preview-pool/config', xdgDataHome: '/mnt/preview-pool/data', xdgCacheHome: '/mnt/preview-pool/cache', futureRuntime: { keep: true } };
  return data;
}
