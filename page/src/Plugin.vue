<script setup>
import { computed, onUnmounted, ref, watch } from 'vue';
import FilePicker from './FilePicker.vue';
import Modal from './Modal.vue';
import { request } from './api.js';
import { absolutePath, argumentLines, availableDiscoveries, clone, editApp, exitLabel, newApp, processFor } from './model.js';
import { useManager } from './useManager.js';

defineProps({ plugin: { type: Object, default: () => ({}) } });
const { snapshot, syncing, mutating, stale, error, notice, lastUpdated, loaded, disabled, refresh, mutate } = useManager();
const apps = computed(() => snapshot.value?.config.apps || []);
const discovered = computed(() => availableDiscoveries(snapshot.value));
const runningCount = computed(() => snapshot.value?.apps.filter(app => app.running).length || 0);
const bootCount = computed(() => apps.value.filter(app => app.autostart).length);
const folderDraft = ref('');
const folderBase = ref(null);
const manualPath = ref('');
const picker = ref(null);
const editor = ref(null);
const editorError = ref('');
const removal = ref(null);
const logApp = ref(null);
const logText = ref('');
const logError = ref('');
const logBusy = ref(false);
let logController;
let logGeneration = 0;
let live = true;
watch(() => snapshot.value?.config.folder, value => { if (!folderBase.value) folderDraft.value = value || ''; });
const status = id => processFor(snapshot.value, id);
const timeLabel = computed(() => lastUpdated.value?.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) || '');
function draftFolder(path) { if (!folderBase.value && snapshot.value) folderBase.value = clone(snapshot.value.config); folderDraft.value = path; }
function resetFolder() { folderBase.value = null; folderDraft.value = snapshot.value?.config.folder || ''; }
async function saveFolder() {
  if (!absolutePath(folderDraft.value, { optional: true })) { error.value = 'Enter an absolute folder path, beginning with /, or leave it empty.'; return; }
  const base = folderBase.value || snapshot.value.config;
  const ok = await mutate({ action: 'save', expectedRevision: base.revision, config: { ...base, folder: folderDraft.value } }, 'Folder saved. Executables are listed below; none have been started.');
  if (ok) resetFolder();
  else if (!stale.value) { folderBase.value = clone(snapshot.value.config); }
}
function selectedPath(path) {
  if (picker.value === 'folder') draftFolder(path);
  else if (picker.value === 'workdir' && editor.value) editor.value.workdir = path;
  else if (picker.value === 'editPath' && editor.value) editor.value.path = path;
  else manualPath.value = path;
  picker.value = null;
}
async function add(path) {
  try {
    if (disabled.value) return;
    if (apps.value.some(app => app.path === path)) throw new Error('This executable is already in your apps.');
    const config = clone(snapshot.value.config);
    config.apps.push(newApp(path));
    if (await mutate({ action: 'save', expectedRevision: config.revision, config }, 'App added. Turn on Keep running when you’re ready to start it.')) manualPath.value = '';
  } catch (failure) { error.value = failure.message; }
}
async function toggle(app, event) {
  const enabled = event.target.checked;
  event.target.checked = status(app.id).desired;
  await mutate({ action: 'toggle', id: app.id, enabled }, '');
}
async function boot(app, event) {
  const value = event.target.checked;
  event.target.checked = app.autostart;
  const config = editApp(snapshot.value.config, app.id, { autostart: value });
  await mutate({ action: 'save', expectedRevision: config.revision, config }, value ? `${app.name} will start at the next boot.` : `${app.name} will not start at the next boot.`);
}
function openEditor(app) {
  editorError.value = '';
  editor.value = { ...clone(app), base: clone(snapshot.value.config), argumentsText: app.args.join('\n'), argumentsEdited: false };
}
async function saveEditor() {
  if (!editor.value || disabled.value) return;
  const draft = editor.value;
  try {
    if (!draft.name.trim()) throw new Error('Give this app a name.');
    if (!absolutePath(draft.path) || !absolutePath(draft.workdir, { optional: true })) throw new Error('Executable and working directory paths must begin with /.');
    const config = editApp(draft.base, draft.id, { name: draft.name, path: draft.path, workdir: draft.workdir, args: draft.argumentsEdited ? argumentLines(draft.argumentsText) : draft.args });
    if (await mutate({ action: 'save', expectedRevision: draft.base.revision, config }, 'App configuration saved.')) editor.value = null;
    else {
      editorError.value = error.value;
      // Keep the user's draft visible but require an explicit reopen after a
      // failed CAS; never silently rebase an old form over newer app changes.
      draft.failed = true;
    }
  } catch (failure) { editorError.value = failure.message; }
}
async function removeApp() {
  if (!removal.value) return;
  const app = removal.value;
  if (await mutate({ action: 'remove', id: app.id, expectedRevision: app.revision }, `${app.name} was removed from managed apps. Its executable was not deleted.`)) removal.value = null;
  else removal.value = null;
}
async function loadLogs(app = logApp.value) {
  if (!app) return;
  const mine = ++logGeneration;
  logController?.abort();
  logController = new AbortController();
  logApp.value = app;
  logText.value = '';
  logError.value = '';
  logBusy.value = true;
  try {
    const value = await request({ action: 'logs', id: app.id }, { signal: logController.signal });
    if (typeof value.lines !== 'string') throw new Error('The host returned an invalid log response.');
    if (live && mine === logGeneration) logText.value = value.lines.slice(-65536);
  } catch (failure) {
    if (live && mine === logGeneration && failure.name !== 'AbortError') logError.value = failure.message;
  } finally { if (live && mine === logGeneration) logBusy.value = false; }
}
function closeLogs() { logGeneration++; logController?.abort(); logApp.value = null; logBusy.value = false; }
onUnmounted(() => { live = false; logGeneration++; logController?.abort(); });
</script>

<template>
  <section class="bm" aria-labelledby="bm-title">
    <header class="bm-header">
      <div class="bm-title-group"><span class="bm-brand" aria-hidden="true">›_</span><div><div class="bm-eyebrow">MOS / UTILITIES</div><h2 id="bm-title">Binary Manager</h2><p class="bm-subtitle">Your executables. One place to keep them running.</p></div></div>
      <div class="bm-header-status"><span class="bm-live-dot" :class="{ 'bm-dot-stale': stale || !loaded }"></span><span>{{ stale ? 'Status unavailable' : loaded ? 'Live status' : 'Connecting to host' }}</span><button type="button" class="bm-small" :disabled="syncing || mutating" @click="refresh()">{{ syncing ? 'Refreshing…' : 'Refresh' }}</button></div>
    </header>

    <aside class="bm-trust"><span aria-hidden="true">!</span><p><strong>Run only binaries you trust.</strong> Apps run with the MOS API’s privileges, typically root, without a sandbox. Use foreground programs; apps that daemonize are not supported.</p></aside>
    <div v-if="error" class="bm-feedback bm-error" role="alert"><div><strong>{{ stale ? 'Host status needs attention' : 'Couldn’t complete that change' }}</strong><p>{{ error }}</p></div><button type="button" :disabled="syncing || mutating" @click="refresh()">Retry status</button></div>
    <p v-if="notice" role="status" class="bm-feedback bm-success">{{ notice }}</p>

    <div class="bm-stats" aria-label="App summary"><div><span class="bm-stat-number">{{ loaded ? runningCount : '—' }}</span><span>Running now</span></div><div><span class="bm-stat-number">{{ loaded ? apps.length : '—' }}</span><span>Managed apps</span></div><div><span class="bm-stat-number">{{ loaded ? bootCount : '—' }}</span><span>Start on boot</span></div><p class="bm-stats-note">{{ mutating ? 'Applying change…' : stale ? 'Showing last known state. Controls are paused.' : loaded ? `Updated ${timeLabel} · every 3 seconds` : 'Loading configuration…' }}</p></div>

    <div class="bm-source-grid">
      <section class="bm-card" aria-labelledby="bm-folder-title"><header class="bm-card-heading"><span class="bm-section-icon" aria-hidden="true">▱</span><div><h3 id="bm-folder-title">Watch a folder</h3><p>List executable files directly inside a host folder.</p></div></header>
        <form @submit.prevent="saveFolder"><fieldset :disabled="disabled"><label for="bm-folder">Folder path</label><div class="bm-input-action"><input id="bm-folder" :value="folderDraft" placeholder="/mnt/user/applications" type="text" autocomplete="off" spellcheck="false" @input="draftFolder($event.target.value)"><button type="button" @click="picker = 'folder'">Browse</button></div><div class="bm-form-footer"><span class="bm-muted">{{ folderBase ? 'Unsaved folder change' : snapshot?.config.folder ? 'Folder saved' : 'No folder selected' }}</span><div class="bm-button-group"><button v-if="folderBase" type="button" class="bm-text-button" @click="resetFolder">Cancel</button><button type="submit" class="bm-primary" :disabled="!folderBase">Save folder</button></div></div></fieldset></form>
      </section>
      <section class="bm-card" aria-labelledby="bm-add-title"><header class="bm-card-heading"><span class="bm-section-icon" aria-hidden="true">+</span><div><h3 id="bm-add-title">Add an executable</h3><p>Pick a binary anywhere on the MOS host.</p></div></header>
        <form @submit.prevent="add(manualPath)"><fieldset :disabled="disabled"><label for="bm-manual">Executable path</label><div class="bm-input-action"><input id="bm-manual" v-model="manualPath" placeholder="/opt/my-app/server" type="text" autocomplete="off" spellcheck="false"><button type="button" @click="picker = 'file'">Browse</button></div><div class="bm-form-footer"><span class="bm-muted">Adding an app does not start it</span><button type="submit" class="bm-primary" :disabled="!manualPath">Add app</button></div></fieldset></form>
      </section>
    </div>

    <section class="bm-app-section" aria-labelledby="bm-apps-title"><header class="bm-section-heading"><div><h3 id="bm-apps-title">Managed apps <span class="bm-count">{{ apps.length }}</span></h3><p>Keep running controls this boot session. Start on boot is saved independently.</p></div></header>
      <div v-if="!loaded" class="bm-empty" role="status"><span class="bm-empty-mark" aria-hidden="true">›_</span><h4>{{ stale ? 'Unable to load your apps' : 'Connecting to Binary Manager…' }}</h4><p>{{ stale ? 'Use Retry status above to reconnect. No settings have been changed.' : 'Reading host configuration and process status.' }}</p></div>
      <div v-else-if="!apps.length" class="bm-empty"><span class="bm-empty-mark" aria-hidden="true">›_</span><h4>Your first app starts here</h4><p>Choose a folder or add an executable, then turn on Keep running.</p></div>
      <div v-else class="bm-app-list" :aria-busy="mutating">
        <article v-for="app in apps" :key="app.id" class="bm-app" :data-app-id="app.id"><div class="bm-app-main"><span class="bm-app-icon" aria-hidden="true">›_</span><div class="bm-app-identity"><div class="bm-app-name"><h4>{{ app.name }}</h4><span class="bm-badge" :class="`bm-state-${status(app.id).state}`">{{ status(app.id).state }}</span></div><p class="bm-mono bm-app-path" :title="app.path">{{ app.path }}</p><p v-if="status(app.id).running || status(app.id).restarts || status(app.id).lastExit" class="bm-process-detail"><span v-if="status(app.id).running">PID {{ status(app.id).pid }}</span><span v-if="status(app.id).restarts">{{ status(app.id).restarts }} restart{{ status(app.id).restarts === 1 ? '' : 's' }}</span><span v-if="status(app.id).lastExit">{{ exitLabel(status(app.id).lastExit) }}</span></p><p v-if="status(app.id).error" class="bm-app-error">{{ status(app.id).error }}</p></div></div>
          <div class="bm-app-controls"><label class="bm-switch-label"><input type="checkbox" role="switch" :checked="status(app.id).desired" :disabled="disabled" :aria-label="`Keep ${app.name} running`" aria-describedby="bm-running-help" @change="toggle(app, $event)"><span class="bm-switch" aria-hidden="true"></span><span>Keep running</span></label><label class="bm-boot-label"><input type="checkbox" :checked="app.autostart" :disabled="disabled" :aria-label="`Start ${app.name} on boot`" @change="boot(app, $event)"> Start on boot</label></div>
          <div class="bm-app-actions"><button type="button" :disabled="disabled" :aria-label="`Restart ${app.name}`" @click="mutate({ action: 'restart', id: app.id }, `${app.name} restart requested.`)">Restart</button><button type="button" :disabled="!loaded || stale" :aria-label="`View ${app.name} logs`" @click="loadLogs(app)">Logs</button><button type="button" :disabled="disabled" :aria-label="`Edit ${app.name}`" @click="openEditor(app)">Edit</button><button type="button" class="bm-remove" :disabled="disabled" :aria-label="`Remove ${app.name}`" @click="removal = { ...app, revision: snapshot.config.revision }">Remove</button></div>
        </article>
      </div>
      <p id="bm-running-help" class="bm-control-help">Failed processes restart with a 1–30 second backoff. A clean exit stops the app. Turning Keep running off stops it now and leaves its boot preference unchanged.</p>
    </section>

    <section v-if="loaded && snapshot.config.folder" class="bm-discovery" aria-labelledby="bm-discovery-title"><header class="bm-section-heading"><div><h3 id="bm-discovery-title">Found in your folder <span class="bm-count">{{ discovered.length }}</span></h3><p class="bm-mono">{{ snapshot.config.folder }}</p></div><span class="bm-muted">Not started automatically</span></header><p v-if="snapshot.folderError" role="alert" class="bm-error">{{ snapshot.folderError }}</p><p v-if="snapshot.discoveryTruncated" class="bm-error">This folder has more entries than the host listing limit. Some files are not shown; use a smaller folder or add an executable by path.</p><ul v-if="!snapshot.folderError && discovered.length" class="bm-discovery-list"><li v-for="entry in discovered" :key="entry.path"><span class="bm-file-icon" aria-hidden="true">›_</span><div><strong>{{ entry.name }}</strong><p class="bm-mono">{{ entry.path }}</p></div><button type="button" :disabled="disabled" :aria-label="`Add ${entry.name}`" @click="add(entry.path)">+ Add</button></li></ul><p v-else-if="!snapshot.folderError" class="bm-discovery-empty">{{ snapshot.discovered.length ? 'All executables in this folder are already managed.' : 'No executable files found. Check the folder and file permissions.' }}</p></section>
    <footer class="bm-page-footer"><span>Binary Manager <span class="bm-muted">/ MOS</span></span><span v-if="loaded" class="bm-muted">Configuration revision {{ snapshot.config.revision }}</span></footer>

    <FilePicker v-if="picker" :mode="picker === 'folder' || picker === 'workdir' ? 'folder' : 'file'" @close="picker = null" @select="selectedPath" />
    <Modal v-if="editor && !picker" :title="`Edit ${editor.name || 'app'}`" :busy="mutating" @close="editor = null"><form @submit.prevent="saveEditor"><fieldset class="bm-editor-fields" :disabled="disabled || editor.failed"><label for="bm-edit-name">App name<input id="bm-edit-name" v-model="editor.name" type="text" required maxlength="160"></label><label for="bm-edit-path">Executable path</label><div class="bm-input-action"><input id="bm-edit-path" v-model="editor.path" type="text" required spellcheck="false"><button type="button" @click="picker = 'editPath'">Browse</button></div><label for="bm-edit-args">Arguments <span class="bm-muted">one per line</span><textarea id="bm-edit-args" v-model="editor.argumentsText" rows="5" spellcheck="false" @input="editor.argumentsEdited = true"></textarea></label><p class="bm-field-help">Each line is passed literally as one argument. Do not add shell quotes. Empty text means no arguments; empty lines between arguments are kept.</p><label for="bm-edit-cwd">Working directory <span class="bm-muted">optional</span></label><div class="bm-input-action"><input id="bm-edit-cwd" v-model="editor.workdir" type="text" placeholder="Default: executable’s folder" spellcheck="false"><button type="button" @click="picker = 'workdir'">Browse</button></div><p class="bm-field-help">Saving executable, argument or working directory changes restarts an app with Keep running enabled. Changes to a stopped app take effect when you start it.</p></fieldset><p v-if="editorError" role="alert" class="bm-error">{{ editorError }}<span v-if="editor.failed"> Close and reopen this editor to review the latest settings.</span></p><footer class="bm-modal-actions"><button type="button" :disabled="mutating" @click="editor = null">Cancel</button><button type="submit" class="bm-primary" :disabled="disabled || editor.failed">{{ mutating ? 'Saving…' : 'Save changes' }}</button></footer></form></Modal>
    <Modal v-if="removal" :title="`Remove ${removal.name}?`" :busy="mutating" @close="removal = null"><p class="bm-modal-intro">This stops the managed process and removes its saved settings. The executable file stays on your host.</p><p class="bm-mono bm-wrap">{{ removal.path }}</p><footer class="bm-modal-actions"><button type="button" :disabled="mutating" @click="removal = null">Cancel</button><button type="button" class="bm-danger" :disabled="disabled" @click="removeApp">{{ mutating ? 'Removing…' : 'Stop and remove' }}</button></footer></Modal>
    <Modal v-if="logApp" :title="`${logApp.name} · recent logs`" wide @close="closeLogs"><p class="bm-muted bm-modal-intro">Bounded recent output from the host. Logs may contain information printed by the app.</p><p v-if="logError" role="alert" class="bm-error">{{ logError }}</p><pre class="bm-logs" :aria-busy="logBusy">{{ logBusy ? 'Loading recent output…' : logText || 'No recent output.' }}</pre><footer class="bm-modal-actions"><button type="button" @click="closeLogs">Close</button><button type="button" :disabled="logBusy" @click="loadLogs()">Refresh logs</button></footer></Modal>
  </section>
</template>

<style src="./style.css"></style>
