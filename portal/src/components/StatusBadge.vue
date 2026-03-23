<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  status: string
  connected?: boolean
}>()

const colorClass = computed(() => {
  if (props.connected === false) return 'bg-red-100 text-red-800'
  switch (props.status?.toLowerCase()) {
    case 'ready':
      return 'bg-green-100 text-green-800'
    case 'scheduling':
    case 'pending':
      return 'bg-yellow-100 text-yellow-800'
    case 'active':
      return 'bg-green-100 text-green-800'
    case 'terminating':
      return 'bg-red-100 text-red-800'
    default:
      return 'bg-gray-100 text-gray-800'
  }
})
</script>

<template>
  <span
    class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium"
    :class="colorClass"
  >
    {{ status }}
  </span>
</template>
