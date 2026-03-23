<script setup lang="ts">
import { computed } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useGraphQLQuery } from '@/composables/useGraphQL'
import { LIST_EDGES, type ListEdgesResult } from '@/graphql/queries/edges'

const { data, loading, error } = useGraphQLQuery<ListEdgesResult>(LIST_EDGES, undefined, 10000)

const edges = computed(() => data.value?.kedge_faros_sh?.v1alpha1?.Edges?.items ?? [])

const stats = computed(() => {
  const items = edges.value
  const total = items.length
  const ready = items.filter((e) => e.status?.phase === 'Ready').length
  const connected = items.filter((e) => e.status?.connected).length
  return { total, ready, connected, disconnected: total - connected }
})
</script>

<template>
  <AppLayout>
    <h1 class="text-xl font-semibold text-gray-900">Dashboard</h1>

    <div v-if="error" class="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</div>
    <div v-else-if="loading && !data" class="mt-4 text-sm text-gray-500">Loading...</div>

    <div v-else class="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <div class="rounded-lg border border-gray-200 bg-white p-5">
        <p class="text-sm font-medium text-gray-500">Total Edges</p>
        <p class="mt-1 text-2xl font-semibold text-gray-900">{{ stats.total }}</p>
      </div>
      <div class="rounded-lg border border-gray-200 bg-white p-5">
        <p class="text-sm font-medium text-gray-500">Ready</p>
        <p class="mt-1 text-2xl font-semibold text-green-700">{{ stats.ready }}</p>
      </div>
      <div class="rounded-lg border border-gray-200 bg-white p-5">
        <p class="text-sm font-medium text-gray-500">Connected</p>
        <p class="mt-1 text-2xl font-semibold text-blue-700">{{ stats.connected }}</p>
      </div>
      <div class="rounded-lg border border-gray-200 bg-white p-5">
        <p class="text-sm font-medium text-gray-500">Disconnected</p>
        <p class="mt-1 text-2xl font-semibold text-red-700">{{ stats.disconnected }}</p>
      </div>
    </div>

    <div v-if="edges.length > 0" class="mt-8">
      <h2 class="text-sm font-medium text-gray-700">Recent Edges</h2>
      <div class="mt-3 space-y-2">
        <router-link
          v-for="edge in edges.slice(0, 5)"
          :key="edge.metadata.name"
          :to="`/edges/${edge.metadata.name}`"
          class="flex items-center justify-between rounded-md border border-gray-200 bg-white px-4 py-3 text-sm hover:bg-gray-50"
        >
          <div class="flex items-center gap-3">
            <span class="font-medium text-gray-900">{{ edge.metadata.name }}</span>
            <span class="text-gray-400">{{ edge.spec?.type }}</span>
          </div>
          <StatusBadge :status="edge.status?.phase" :connected="edge.status?.connected" />
        </router-link>
      </div>
    </div>
  </AppLayout>
</template>
