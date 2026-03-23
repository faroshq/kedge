<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import ResourceTable from '@/components/ResourceTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useGraphQLQuery } from '@/composables/useGraphQL'
import { LIST_EDGES, type ListEdgesResult, type EdgeItem } from '@/graphql/queries/edges'

const router = useRouter()
const { data, loading, error } = useGraphQLQuery<ListEdgesResult>(LIST_EDGES, undefined, 10000)

const columns = [
  { key: 'name', label: 'Name' },
  { key: 'type', label: 'Type' },
  { key: 'phase', label: 'Phase' },
  { key: 'connected', label: 'Connected' },
  { key: 'hostname', label: 'Hostname' },
  { key: 'agentVersion', label: 'Agent Version' },
  { key: 'age', label: 'Age' },
]

function formatAge(timestamp: string): string {
  const diff = Date.now() - new Date(timestamp).getTime()
  const hours = Math.floor(diff / 3600000)
  if (hours < 1) return `${Math.floor(diff / 60000)}m`
  if (hours < 24) return `${hours}h`
  return `${Math.floor(hours / 24)}d`
}

const rows = computed(() =>
  (data.value?.kedge_faros_sh?.v1alpha1?.Edges?.items ?? []).map((e: EdgeItem) => ({
    name: e.metadata.name,
    type: e.spec?.type ?? '',
    phase: e.status?.phase ?? 'Unknown',
    connected: e.status?.connected ?? false,
    hostname: e.status?.hostname ?? '',
    agentVersion: e.status?.agentVersion ?? '',
    age: formatAge(e.metadata.creationTimestamp),
    _raw: e,
  })),
)

function handleRowClick(row: Record<string, unknown>) {
  router.push(`/edges/${row.name}`)
}
</script>

<template>
  <AppLayout>
    <h1 class="text-xl font-semibold text-gray-900">Edges</h1>

    <div class="mt-4">
      <ResourceTable
        :columns="columns"
        :rows="rows"
        :loading="loading && !data"
        :error="error"
        @row-click="handleRowClick"
      >
        <template #phase="{ value, row }">
          <StatusBadge :status="value as string" :connected="row.connected as boolean" />
        </template>
        <template #connected="{ value }">
          <span :class="value ? 'text-green-600' : 'text-red-500'" class="text-sm">
            {{ value ? 'Yes' : 'No' }}
          </span>
        </template>
      </ResourceTable>
    </div>
  </AppLayout>
</template>
