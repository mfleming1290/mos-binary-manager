<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue';
import Modal from './Modal.vue';
import { request } from './api.js';
import { absolutePath, validateBrowse } from './model.js';
const props = defineProps({ mode: { type: String, default: 'file' } });
const emit = defineEmits(['select', 'close']);
const listing = ref(null);
const pathInput = ref('/');
const busy = ref(false);
const error = ref('');
const entries = computed(() => (listing.value?.entries || []).filter(entry => props.mode === 'file' || entry.type === 'directory'));
let controller;
let generation = 0;
let live = true;
async function browse(path) {
  if (!absolutePath(path)) { error.value = 'Enter an absolute folder path, beginning with /.'; return; }
  const mine = ++generation;
  controller?.abort();
  controller = new AbortController();
  busy.value = true;
  error.value = '';
  try {
    const result = validateBrowse(await request({ action: 'browse', path }, { signal: controller.signal }));
    if (!live || mine !== generation) return;
    listing.value = result;
    pathInput.value = result.path;
  } catch (failure) {
    if (live && mine === generation && failure.name !== 'AbortError') error.value = failure.message;
  } finally { if (live && mine === generation) busy.value = false; }
}
onMounted(() => browse('/'));
onUnmounted(() => { live = false; generation++; controller?.abort(); });
</script>
<template>
  <Modal :title="mode === 'file' ? 'Choose a host executable' : 'Choose a host folder'" @close="emit('close')">
    <p class="bm-muted bm-modal-intro">Browsing the MOS host. Only folders and executable files are shown.</p>
    <form class="bm-path-form" @submit.prevent="browse(pathInput)">
      <label class="bm-sr-only" for="bm-browser-path">Host folder path</label>
      <input id="bm-browser-path" v-model="pathInput" type="text" spellcheck="false" autocomplete="off" :disabled="busy">
      <button type="submit" :disabled="busy">Go</button>
    </form>
    <div class="bm-browser-toolbar"><button type="button" :disabled="busy || !listing?.parent || listing.parent === listing.path" @click="browse(listing.parent)">↑ Up</button><span class="bm-mono">{{ listing?.path || '/' }}</span></div>
    <p v-if="error" role="alert" class="bm-error">{{ error }}</p>
    <p v-if="busy" role="status" class="bm-browser-state">Loading folder…</p>
    <ul v-else class="bm-browser-list" aria-label="Host directory entries">
      <li v-for="entry in entries" :key="entry.path"><button type="button" @click="entry.type === 'directory' ? browse(entry.path) : emit('select', entry.path)"><span aria-hidden="true" class="bm-file-icon">{{ entry.type === 'directory' ? '▱' : '›_' }}</span><span>{{ entry.name }}</span><span class="bm-muted bm-entry-type">{{ entry.type === 'directory' ? 'Folder →' : 'Executable' }}</span></button></li>
      <li v-if="!entries.length" class="bm-browser-state">{{ error ? 'Enter another folder path or try again.' : 'No matching entries in this folder.' }}</li>
    </ul>
    <p v-if="listing?.truncated" class="bm-error">This directory exceeds the host listing limit. Some entries are hidden; enter a narrower folder path.</p>
    <footer class="bm-modal-actions"><button type="button" @click="emit('close')">Cancel</button><button v-if="mode !== 'file'" type="button" class="bm-primary" :disabled="busy || !!error || !listing" @click="emit('select', listing.path)">Use this folder</button></footer>
  </Modal>
</template>
