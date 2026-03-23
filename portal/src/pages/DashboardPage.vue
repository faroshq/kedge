<script setup lang="ts">
import { computed } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useGraphQLQuery } from '@/composables/useGraphQL'
import { LIST_EDGES, type ListEdgesResult } from '@/graphql/queries/edges'
import { Server, Wifi, WifiOff, CheckCircle, Loader2, ChevronRight } from 'lucide-vue-next'

const { data, loading, error } = useGraphQLQuery<ListEdgesResult>(LIST_EDGES, undefined, 10000)

const edges = computed(() => data.value?.kedge_faros_sh?.v1alpha1?.Edges?.items ?? [])

const stats = computed(() => {
  const items = edges.value
  const total = items.length
  const ready = items.filter((e) => e.status?.phase === 'Ready').length
  const connected = items.filter((e) => e.status?.connected).length
  return { total, ready, connected, disconnected: total - connected }
})

const statCards = computed(() => [
  { label: 'Total Edges', value: stats.value.total, icon: Server, color: 'text-accent', bg: 'bg-accent-subtle' },
  { label: 'Ready', value: stats.value.ready, icon: CheckCircle, color: 'text-success', bg: 'bg-success-subtle' },
  { label: 'Connected', value: stats.value.connected, icon: Wifi, color: 'text-accent', bg: 'bg-accent-subtle' },
  { label: 'Disconnected', value: stats.value.disconnected, icon: WifiOff, color: 'text-danger', bg: 'bg-danger-subtle' },
])
</script>

<template>
  <AppLayout>
    <div class="flex items-center justify-between">
      <h1 class="text-lg font-semibold tracking-tight text-text-primary">Dashboard</h1>
    </div>

    <div v-if="error" class="mt-4 flex items-center gap-2 rounded-lg bg-danger-subtle p-3 text-[13px] text-danger">
      {{ error }}
    </div>

    <div v-else-if="loading && !data" class="mt-12 flex flex-col items-center justify-center">
      <Loader2 class="h-6 w-6 animate-spin text-text-muted" :stroke-width="1.75" />
      <p class="mt-3 text-[13px] text-text-muted">Loading dashboard...</p>
    </div>

    <template v-else>
      <!-- Stat Cards -->
      <div class="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div
          v-for="card in statCards"
          :key="card.label"
          class="group rounded-xl border border-border-subtle bg-surface-raised p-5 transition-all duration-200 hover:border-border-default hover:shadow-lg hover:shadow-black/10"
        >
          <div class="flex items-center justify-between">
            <p class="text-[12px] font-medium uppercase tracking-wider text-text-muted">{{ card.label }}</p>
            <div class="flex h-8 w-8 items-center justify-center rounded-lg transition-colors duration-200" :class="card.bg">
              <component :is="card.icon" class="h-4 w-4" :class="card.color" :stroke-width="1.75" />
            </div>
          </div>
          <p class="mt-3 text-3xl font-bold tabular-nums tracking-tight text-text-primary">{{ card.value }}</p>
        </div>
      </div>

      <!-- Recent Edges -->
      <div v-if="edges.length > 0" class="mt-8">
        <div class="flex items-center justify-between">
          <h2 class="text-[13px] font-semibold uppercase tracking-wider text-text-muted">Recent Edges</h2>
          <router-link to="/edges" class="text-[12px] font-medium text-accent transition-colors hover:text-accent-hover">
            View all
          </router-link>
        </div>
        <div class="mt-3 space-y-1.5">
          <router-link
            v-for="edge in edges.slice(0, 5)"
            :key="edge.metadata.name"
            :to="`/edges/${edge.metadata.name}`"
            class="group flex items-center justify-between rounded-xl border border-border-subtle bg-surface-raised px-4 py-3 transition-all duration-150 hover:border-border-default hover:bg-surface-hover"
          >
            <div class="flex items-center gap-3">
              <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-surface-overlay">
                <Server class="h-4 w-4 text-text-muted" :stroke-width="1.75" />
              </div>
              <div>
                <span class="text-[13px] font-medium text-text-primary">{{ edge.metadata.name }}</span>
                <span class="ml-2 text-[12px] text-text-muted">{{ edge.spec?.type }}</span>
              </div>
            </div>
            <div class="flex items-center gap-3">
              <StatusBadge :status="edge.status?.phase" :connected="edge.status?.connected" />
              <ChevronRight class="h-4 w-4 text-text-muted/50 transition-transform duration-150 group-hover:translate-x-0.5 group-hover:text-text-muted" :stroke-width="1.75" />
            </div>
          </router-link>
        </div>
      </div>
    </template>
  </AppLayout>
</template>
