export async function api(path, { method = 'GET', body, signal } = {}) {
  const token = localStorage.getItem('authToken');
  if (!token) throw new Error('Sign in to MOS before using this plugin');
  const response = await fetch(`/api/v1/mos/plugins${path}`, {
    method, signal,
    headers: { Authorization: `Bearer ${token}`, ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const data = await response.json().catch(() => null);
  if (!response.ok) throw new Error(data?.error || `MOS request failed (${response.status})`);
  return data;
}

// The MOS query transport constructs a shell command. Only a fixed command and a
// base64url JSON argument cross that boundary; paths/arguments are never shell text.
export function encodeRequest(value) {
  const bytes = new TextEncoder().encode(JSON.stringify(value));
  if (bytes.byteLength > 65536) throw new Error('This request exceeds the 64 KiB limit. Shorten the app configuration or arguments.');
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '');
}

export function decodeQuery(result) {
  if (result?.timed_out === true) throw Object.assign(new Error('The host request timed out. Refresh status before trying again.'), { code: 'TIMEOUT' });
  if (result?.success !== true || result.exit_code !== 0) {
    throw Object.assign(new Error(typeof result?.output === 'string' ? result.output.slice(0, 500) : 'MOS could not complete the host request.'), { code: 'QUERY_FAILED' });
  }
  const output = result.output;
  if (!output || typeof output !== 'object' || Array.isArray(output) || typeof output.ok !== 'boolean') throw new Error('MOS returned an invalid Binary Manager response.');
  if (!output.ok) throw Object.assign(new Error(typeof output.error === 'string' ? output.error : 'The host rejected this request.'), { code: output.code });
  return output;
}

export async function request(payload, { signal } = {}) {
  const controller = new AbortController();
  const cancel = () => controller.abort();
  signal?.addEventListener('abort', cancel, { once: true });
  if (signal?.aborted) cancel();
  let timedOut = false;
  const timer = setTimeout(() => { timedOut = true; controller.abort(); }, 20000);
  try {
    return decodeQuery(await api('/query', {
      method: 'POST', signal: controller.signal,
      body: { command: 'binary-manager', args: ['request', encodeRequest(payload)], timeout: 15, parse_json: true },
    }));
  } catch (error) {
    if (timedOut) throw Object.assign(new Error('The host request timed out. Refresh status before trying again.'), { code: 'TIMEOUT' });
    throw error;
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener('abort', cancel);
  }
}
