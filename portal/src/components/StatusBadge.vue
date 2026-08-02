<script setup lang="ts">
import { computed } from 'vue'

type Tone = 'success' | 'warning' | 'danger' | 'muted'

const props = withDefaults(
  defineProps<{
    status: string
    connected?: boolean | null
    tone?: Tone | null
  }>(),
  { connected: null, tone: null },
)

const toneClass: Record<Tone, string> = {
  success: 'k-badge--success',
  warning: 'k-badge--warning',
  danger: 'k-badge--danger',
  muted: 'k-badge--muted',
}

const cls = computed(() => {
  if (props.connected === false) return 'k-badge--danger'

  if (props.tone) return toneClass[props.tone]

  switch (props.status?.toLowerCase()) {
    case 'ready':
    case 'succeeded':
    case 'committed':
    case 'active':
      return 'k-badge--success'
    case 'scheduling':
    case 'pending':
    case 'provisioning':
    case 'running':
    case 'status unavailable':
      return 'k-badge--warning'
    case 'terminating':
    case 'failed':
    case 'error':
    case 'repository missing':
    case 'connection missing':
      return 'k-badge--danger'
    default:
      return 'k-badge--muted'
  }
})
</script>

<template>
  <span class="k-badge" :class="cls">
    <span class="relative flex h-1.5 w-1.5">
      <span
        v-if="status?.toLowerCase() === 'ready' && connected !== false"
        class="live-dot k-badge__dot absolute inline-flex h-full w-full"
      />
      <span class="k-badge__dot relative inline-flex" />
    </span>
    {{ status }}
  </span>
</template>
