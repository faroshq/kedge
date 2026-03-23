<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import ResourceTable from '@/components/ResourceTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useGraphQLQuery } from '@/composables/useGraphQL'
import { LIST_EDGES, type ListEdgesResult, type EdgeItem } from '@/graphql/queries/edges'
import { Server, Wifi, WifiOff } from 'lucide-vue-next'

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
    <div class="flex items-center gap-3">
      <div class="flex h-9 w-9 items-center justify-center rounded-lg bg-accent-subtle">
        <Server class="h-4.5 w-4.5 text-accent" :stroke-width="1.75" />
      </div>
      <div>
        <h1 class="text-lg font-semibold tracking-tight text-text-primary">Edges</h1>
        <p class="text-[12px] text-text-muted">{{ rows.length }} edge{{ rows.length !== 1 ? 's' : '' }} registered</p>
      </div>
    </div>

    <div class="mt-5">
      <ResourceTable
        :columns="columns"
        :rows="rows"
        :loading="loading && !data"
        :error="error"
        @row-click="handleRowClick"
      >
        <template #name="{ value }">
          <span class="font-medium text-text-primary">{{ value }}</span>
        </template>
        <template #type="{ value }">
          <span class="rounded-md bg-surface-overlay px-2 py-0.5 text-[11px] font-medium text-text-secondary">{{ value }}</span>
        </template>
        <template #phase="{ value, row }">
          <StatusBadge :status="value as string" :connected="row.connected as boolean" />
        </template>
        <template #connected="{ value }">
          <div class="flex items-center gap-1.5">
            <component
              :is="value ? Wifi : WifiOff"
              class="h-3.5 w-3.5"
              :class="value ? 'text-success' : 'text-danger'"
              :stroke-width="1.75"
            />
            <span :class="value ? 'text-success' : 'text-danger'" class="text-[13px]">
              {{ value ? 'Yes' : 'No' }}
            </span>
          </div>
        </template>
        <template #age="{ value }">
          <span class="font-mono text-[12px] text-text-muted">{{ value }}</span>
        </template>
      </ResourceTable>
    </div>
  </AppLayout>
</template>
