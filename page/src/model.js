const object = value => !!value && typeof value === 'object' && !Array.isArray(value);
export const clone = value => JSON.parse(JSON.stringify(value));
export function absolutePath(value, { optional = false } = {}) {
  return typeof value === 'string' && ((optional && value === '') || (value.startsWith('/') && !/[\0\r\n]/.test(value)));
}
export function validateConfig(value) {
  if (!object(value) || value.schemaVersion !== 1 || !Number.isSafeInteger(value.revision) || value.revision < 0 || !absolutePath(value.folder, { optional: true }) || !Array.isArray(value.apps)) throw new Error('Unsupported or incomplete host configuration. Reload before making changes.');
  const ids = new Set();
  for (const app of value.apps) {
    if (!object(app) || typeof app.id !== 'string' || !app.id || ids.has(app.id) || !absolutePath(app.path) || typeof app.name !== 'string' || !Array.isArray(app.args) || app.args.some(arg => typeof arg !== 'string' || arg.includes('\0')) || !absolutePath(app.workdir, { optional: true }) || typeof app.autostart !== 'boolean') throw new Error('The host returned an invalid app configuration. Reload before making changes.');
    ids.add(app.id);
  }
  return clone(value);
}
export function validateStatus(value) {
  const config = validateConfig(value?.config);
  if (!Array.isArray(value.apps) || !Array.isArray(value.discovered)) throw new Error('The host returned incomplete status information.');
  for (const app of value.apps) {
    if (!object(app) || typeof app.id !== 'string' || typeof app.desired !== 'boolean' || typeof app.running !== 'boolean' || typeof app.state !== 'string') throw new Error('The host returned invalid process status.');
  }
  for (const app of value.discovered) {
    if (!object(app) || !absolutePath(app.path) || typeof app.name !== 'string') throw new Error('The host returned an invalid discovered executable.');
  }
  return { ...value, config };
}
export function validateBrowse(value) {
  if (!absolutePath(value?.path) || !(value.parent === null || absolutePath(value.parent)) || !Array.isArray(value.entries)) throw new Error('The host returned an invalid directory listing.');
  for (const entry of value.entries) {
    if (!object(entry) || typeof entry.name !== 'string' || !absolutePath(entry.path) || !['directory', 'executable'].includes(entry.type)) throw new Error('The host returned an invalid directory entry.');
  }
  return value;
}
export function argumentLines(value) {
  // Each line is exactly one argv item. Empty interior lines intentionally mean
  // empty arguments; there is no shell parsing, interpolation or trimming.
  return value === '' ? [] : value.split('\n').map(line => line.endsWith('\r') ? line.slice(0, -1) : line);
}
export function generateAppId(crypto = globalThis.crypto) {
  if (typeof crypto?.randomUUID === 'function') return crypto.randomUUID();
  // MOS may be served over LAN HTTP, where randomUUID is unavailable.
  // getRandomValues still provides secure randomness in that context.
  if (typeof crypto?.getRandomValues !== 'function') throw new Error('This browser cannot generate secure random app IDs. Use a current browser.');
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
export function newApp(path, id = generateAppId()) {
  if (!absolutePath(path)) throw new Error('Choose an absolute executable path, beginning with /.');
  return { id, path, name: path.split('/').filter(Boolean).at(-1) || path, args: [], workdir: '', autostart: false };
}
export function availableDiscoveries(status) {
  const paths = new Set(status?.config.apps.map(app => app.path) || []);
  return (status?.discovered || []).filter(app => !paths.has(app.path));
}
export function editApp(config, id, changes) {
  if (!config.apps.some(app => app.id === id)) throw new Error('This app was removed. Reload the list.');
  return validateConfig({ ...config, apps: config.apps.map(app => app.id === id ? { ...app, ...changes } : app) });
}
export function processFor(status, id) {
  return status?.apps.find(app => app.id === id) || { id, desired: false, running: false, state: 'stopped', restarts: 0, lastExit: null, error: '' };
}
export function exitLabel(exit) {
  if (!exit) return '';
  return exit.signal ? `Last exit: ${exit.signal}` : `Last exit: code ${exit.code ?? 'unknown'}`;
}
