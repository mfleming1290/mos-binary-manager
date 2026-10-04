import { createApp } from 'vue';
import Plugin from '../src/Plugin.vue';
import { createMock } from './mock.js';

const real = new URLSearchParams(location.search).get('backend') === 'real';
// Preview is isolated from the MOS application and uses only a local fixture token.
localStorage.setItem('authToken', 'binary-manager-local-preview-only');
const toolbar = document.querySelector('#preview-tools');
const label = document.createElement('strong');
label.textContent = real ? 'LOCAL CLI BRIDGE · dev-only fixture host' : 'LOCAL PREVIEW · simulated MOS host';
toolbar.append(label);
if (!real) {
  const mock = createMock();
  window.fetch = mock.fetch;
  window.binaryManagerPreview = mock;
  const actions = [
    ['Fail next action', () => { mock.failNext = true; }],
    ['Conflict next save', () => { mock.conflictNext = true; }],
    ['Toggle outage', () => { mock.failStatus = !mock.failStatus; label.textContent = mock.failStatus ? 'LOCAL PREVIEW · simulated outage active' : 'LOCAL PREVIEW · simulated MOS host'; }],
    ['Empty apps', () => { mock.value.config.apps = []; mock.value.apps = []; }],
    ['Backoff state', () => { const app = mock.value.apps[0]; if (app) Object.assign(app, { state: 'backoff', running: false, pid: null, restarts: 4, error: 'Process exited with code 1. Retrying shortly.' }); }],
  ];
  for (const [title, action] of actions) { const button = document.createElement('button'); button.textContent = title; button.onclick = action; toolbar.append(button); }
}
createApp(Plugin).mount('#app');
