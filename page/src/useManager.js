import { computed, onMounted, onUnmounted, ref } from 'vue';
import { request } from './api.js';
import { validateStatus } from './model.js';

export function useManager(send = request, interval = 3000) {
  const snapshot = ref(null);
  const syncing = ref(false);
  const mutating = ref(false);
  const stale = ref(false);
  const error = ref('');
  const notice = ref('');
  const lastUpdated = ref(null);
  const loaded = computed(() => snapshot.value !== null);
  const disabled = computed(() => !loaded.value || stale.value || syncing.value || mutating.value);
  const controller = new AbortController();
  let live = true;
  let timer;
  let errorKind = '';
  function apply(result) {
    const next = validateStatus(result);
    if (!live) return;
    snapshot.value = next;
    stale.value = false;
    lastUpdated.value = new Date();
  }
  function schedule() {
    clearTimeout(timer);
    if (live) timer = setTimeout(() => refresh(false), interval);
  }
  async function refresh(explicit = true) {
    if (!live || syncing.value || mutating.value) return false;
    clearTimeout(timer);
    syncing.value = true;
    if (explicit) { error.value = ''; errorKind = ''; notice.value = ''; }
    try {
      const wasStale = stale.value;
      apply(await send({ action: 'status' }, { signal: controller.signal }));
      if (live && wasStale && errorKind === 'mutation') error.value = 'The previous change could not be confirmed. Status is current now; review the result before trying again.';
      if (live && errorKind === 'poll') { error.value = ''; errorKind = ''; }
      return true;
    } catch (failure) {
      if (live && failure.name !== 'AbortError') {
        stale.value = true;
        error.value = failure.message;
        errorKind = 'poll';
      }
      return false;
    } finally {
      if (live) { syncing.value = false; schedule(); }
    }
  }
  async function mutate(payload, successMessage = '') {
    if (disabled.value) return false;
    clearTimeout(timer);
    mutating.value = true;
    error.value = '';
    errorKind = '';
    notice.value = '';
    try {
      apply(await send(payload, { signal: controller.signal }));
      if (live) notice.value = successMessage;
      return true;
    } catch (failure) {
      if (!live || failure.name === 'AbortError') return false;
      // A failed response may still have reached the host. Reconcile first and
      // never replay the mutation automatically, especially add/restart/remove.
      stale.value = true;
      errorKind = 'mutation';
      const conflict = /conflict|revision/i.test(String(failure.code) + ' ' + failure.message);
      error.value = conflict ? 'Configuration changed elsewhere. Your change was not saved. The latest configuration has been reloaded; review it and try again.' : `${failure.message} Checking the host before another change.`;
      try {
        apply(await send({ action: 'status' }, { signal: controller.signal }));
        if (live && !conflict) error.value = `${failure.message} Status has been refreshed. Review the result before trying again.`;
      } catch {
        if (live) error.value = `${failure.message} Status could not be verified. Changes are disabled until a refresh succeeds.`;
      }
      return false;
    } finally {
      if (live) { mutating.value = false; schedule(); }
    }
  }
  onMounted(() => refresh());
  onUnmounted(() => { live = false; clearTimeout(timer); controller.abort(); });
  return { snapshot, syncing, mutating, stale, error, notice, lastUpdated, loaded, disabled, refresh, mutate };
}
