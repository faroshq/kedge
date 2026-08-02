<script setup lang="ts">
import { computed } from 'vue'
import type { Template } from '../types'

const props = defineProps<{ template: Template }>()
defineEmits<{ (e: 'select', name: string): void }>()

// Say up front when this thing will never have a URL. Users otherwise
// provision, wait, and go looking for a link that is never coming. Anything
// that can get a URL (public, or optional's opt-in) needs no pill.
const exposure = computed(() => {
  if ((props.template.exposure || 'internal') !== 'internal') return null
  return { label: 'internal', title: 'No public URL. Reached from inside the platform, authorized per caller.' }
})
</script>

<template>
  <button class="template-card" @click="$emit('select', template.name)">
    <div class="template-card-head">
      <div class="template-card-title">{{ template.displayName || template.name }}</div>
      <span v-if="template.cloud" class="cloud-pill">{{ template.cloud }}</span>
      <span v-if="exposure" class="exposure-pill" :title="exposure.title">{{ exposure.label }}</span>
    </div>
    <p class="template-card-desc">{{ template.description }}</p>
    <div class="template-card-foot">
      <span class="kind">{{ template.kind }}</span>
      <span v-if="template.version" class="version">v{{ template.version }}</span>
    </div>
  </button>
</template>

<style scoped>
.exposure-pill {
  font-size: 0.75rem;
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
  border: 1px solid var(--border, #d0d7de);
  color: var(--muted, #57606a);
  white-space: nowrap;
}
</style>
