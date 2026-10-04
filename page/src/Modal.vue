<script setup>
import { onMounted, ref, useId } from 'vue';
const props = defineProps({ title: { type: String, required: true }, busy: Boolean, wide: Boolean });
const emit = defineEmits(['close']);
const dialog = ref(null);
const titleId = `bm-dialog-${useId()}`;
function cancel(event) { event.preventDefault(); if (!props.busy) emit('close'); }
onMounted(() => dialog.value?.showModal());
</script>
<template>
  <dialog ref="dialog" class="bm-dialog" :class="{ 'bm-dialog-wide': wide }" :aria-labelledby="titleId" @cancel="cancel">
    <header class="bm-modal-heading"><h3 :id="titleId">{{ title }}</h3><button type="button" class="bm-icon-button" aria-label="Close dialog" :disabled="busy" @click="emit('close')">×</button></header>
    <slot />
  </dialog>
</template>
