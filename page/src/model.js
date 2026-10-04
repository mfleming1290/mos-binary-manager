const object = value => !!value && typeof value === 'object' && !Array.isArray(value);
export const clone = value => JSON.parse(JSON.stringify(value));
export function absolutePath(value, { optional = false } = {}) {
  return typeof value === 'string' && ((optional && value === '') || (value.startsWith('/') && !/[\0\r\n]/.test(value)));
}
export function validateConfig(value) {
  if (!object(value) || value.schemaVersion !== 1 || !Number.isSafeInteger(value.revision) || value.revision < 0 || !absolutePath(value.folder, { optional: true }) || !Array.isArray(value.apps)) throw new Error('Unsupported or incomplete host configuration. Reload before making changes.');
  if ('runtimeDefaults' in value) validateRuntimeDefaults(value.runtimeDefaults);
  const ids = new Set();
  for (const app of value.apps) {
    if (!object(app) || typeof app.id !== 'string' || !app.id || ids.has(app.id) || !absolutePath(app.path) || typeof app.name !== 'string' || !Array.isArray(app.args) || app.args.some(arg => typeof arg !== 'string' || arg.includes('\0')) || !absolutePath(app.workdir, { optional: true }) || typeof app.autostart !== 'boolean') throw new Error('The host returned an invalid app configuration. Reload before making changes.');
    if ('runtime' in app && app.runtime !== undefined) validateRuntime(app.runtime, value.runtimeDefaults);
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
  // A binary can back several independently configured instances.
  return status?.discovered || [];
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


// Runtime settings are opt-in. A legacy app has no runtime property at all.
// Unknown fields survive edits, including nested defaults/runtime extensions.
export const runtimeTemplate = () => ({ useDefaults: false, homeMode: 'inherit', home: '', pathDirs: [], env: {}, envFile: '', xdgConfigHome: '', xdgDataHome: '', xdgCacheHome: '' });
export const defaultsTemplate = () => ({ storageRoot: '', pathDirs: [], env: {} });
export function runtimePath(value, { optional = false, pathEntry = false } = {}) {
  if (optional && value === '') return true;
  return absolutePath(value) && new TextEncoder().encode(value).length <= 4096 && !/[\x00-\x1f\x7f]/.test(value)
    && (value === '/' || (!value.endsWith('/') && !value.split('/').slice(1).some(part => part === '' || part === '.' || part === '..')))
    && (!pathEntry || !value.includes(':'));
}
export function validateEnvironment(env) {
  if (!object(env)) throw new Error('Environment settings must be an object.');
  if (Object.keys(env).length > 256) throw new Error('Environment settings are limited to 256 variables.');
  for (const [key, value] of Object.entries(env)) {
    if (key.length > 128 || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) throw new Error('Environment names must contain only letters, numbers and underscores, and cannot start with a number.');
    if (key === 'HOME' || key === 'PATH' || key.startsWith('XDG_') || key.startsWith('BINARY_MANAGER_')) throw new Error('HOME, PATH, XDG_* and BINARY_MANAGER_* cannot be set in ordinary environment settings. Use the dedicated fields where available.');
    if (!(value === null || (typeof value === 'string' && !/[\0\r\n]/.test(value) && new TextEncoder().encode(value).length <= 16384))) throw new Error('Environment values must be single-line literal strings up to 16 KiB or null (unset), without NUL characters.');
  }
  return env;
}
function validatePathDirs(value) {
  if (!Array.isArray(value) || value.length > 64 || value.some(path => !runtimePath(path, { pathEntry: true }))) throw new Error('PATH directories are limited to 64 clean absolute paths without colons or control characters, one per line.');
}
export function validateRuntimeDefaults(value) {
  if (!object(value)) throw new Error('Runtime defaults must be an object.');
  if ('storageRoot' in value && (value.storageRoot === '/' || !runtimePath(value.storageRoot, { optional: true }))) throw new Error('Storage root must be a clean absolute path on mounted persistent pool storage, or empty.');
  if ('pathDirs' in value) validatePathDirs(value.pathDirs);
  if ('env' in value) validateEnvironment(value.env);
  return value;
}
export function validateRuntime(value, defaults) {
  if (!object(value)) throw new Error('Instance runtime settings must be an object.');
  if ('useDefaults' in value && typeof value.useDefaults !== 'boolean') throw new Error('Use shared defaults must be true or false.');
  if ('homeMode' in value && !['inherit', 'managed', 'custom'].includes(value.homeMode)) throw new Error('Choose inherit, managed or custom HOME.');
  for (const key of ['home', 'envFile', 'xdgConfigHome', 'xdgDataHome', 'xdgCacheHome']) {
    if (key in value && !runtimePath(value[key], { optional: true })) throw new Error('HOME, protected environment file and XDG paths must be clean absolute paths or empty.');
  }
  if (value.home === '/') throw new Error('HOME cannot be the filesystem root. Choose a persistent directory.');
  if (value.homeMode === 'managed' && !defaults?.storageRoot) throw new Error('Set a verified persistent storage root in Runtime defaults before using a managed HOME or duplicating an instance.');
  if (value.homeMode === 'custom' && !value.home) throw new Error('A custom HOME needs an existing absolute directory on persistent pool storage.');
  if ('pathDirs' in value) validatePathDirs(value.pathDirs);
  if ('env' in value) validateEnvironment(value.env);
  return value;
}
export function pathDirectoryLines(text) {
  const dirs = argumentLines(text).filter(line => line !== '');
  validatePathDirs(dirs);
  return dirs;
}
export function environmentLines(text) {
  const entries = [];
  const seen = new Set();
  for (const [index, line] of argumentLines(text).entries()) {
    if (line === '') continue;
    const equals = line.indexOf('=');
    let key, value;
    if (line.startsWith('!') && equals < 0) { key = line.slice(1); value = null; }
    else if (equals > 0) { key = line.slice(0, equals); value = line.slice(equals + 1); }
    else throw new Error(`Environment line ${index + 1}: use KEY=value or !KEY to unset. Values are literal; no shell expansion or export syntax.`);
    if (seen.has(key)) throw new Error(`Environment line ${index + 1}: each variable may appear only once.`);
    // Build with entries, not assignment: even an ordinary key such as
    // __proto__ remains data and cannot alter the editor object's prototype.
    validateEnvironment(Object.fromEntries([[key, value]]));
    seen.add(key);
    entries.push([key, value]);
  }
  return validateEnvironment(Object.fromEntries(entries));
}
export function environmentText(env = {}) {
  return Object.entries(env).map(([key, value]) => value === null ? `!${key}` : `${key}=${value}`).join('\n');
}
export function settingsForm(value, template) {
  const settings = { ...template(), ...clone(value || {}) };
  return { ...settings, pathDirsText: settings.pathDirs.join('\n'), environmentText: environmentText(settings.env), pathsEdited: false, environmentEdited: false };
}
export function settingsFromForm(form, original, keys) {
  const settings = { ...clone(original || {}) };
  for (const key of keys) settings[key] = clone(form[key]);
  settings.pathDirs = form.pathsEdited ? pathDirectoryLines(form.pathDirsText) : clone(form.pathDirs);
  settings.env = form.environmentEdited ? environmentLines(form.environmentText) : clone(form.env);
  return settings;
}
export function duplicateApp(config, id, newId = generateAppId()) {
  const source = config.apps.find(app => app.id === id);
  if (!source) throw new Error('This app was removed. Reload the list.');
  if (!config.runtimeDefaults?.storageRoot) throw new Error('Configure Runtime defaults with a persistent pool storage root before duplicating an instance.');
  const duplicate = { ...clone(source), id: newId, name: duplicateName(source.name), autostart: false,
    runtime: { ...runtimeTemplate(), ...clone(source.runtime || {}), useDefaults: source.runtime ? (source.runtime.useDefaults ?? false) : true, homeMode: 'managed', home: '', envFile: '', xdgConfigHome: '', xdgDataHome: '', xdgCacheHome: '' } };
  return validateConfig({ ...config, apps: [...config.apps, duplicate] });
}

export function duplicateName(name) {
  let prefix = '';
  for (const character of name) {
    if (new TextEncoder().encode(prefix + character + ' copy').length > 160) break;
    prefix += character;
  }
  return `${prefix} copy`;
}
