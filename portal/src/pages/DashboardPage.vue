<script setup lang="ts">
import { computed } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useGraphQLQuery } from '@/composables/useGraphQL'
import { LIST_EDGES, type ListEdgesResult } from '@/graphql/queries/edges'
import { Server, Wifi, WifiOff, CheckCircle, ChevronRight, Activity } from 'lucide-vue-next'

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
  { label: 'Total Edges', value: stats.value.total, icon: Server, color: 'text-accent', bg: 'bg-accent-subtle', glow: 'shadow-accent/10' },
  { label: 'Ready', value: stats.value.ready, icon: CheckCircle, color: 'text-success', bg: 'bg-success-subtle', glow: 'shadow-success/10' },
  { label: 'Connected', value: stats.value.connected, icon: Wifi, color: 'text-accent', bg: 'bg-accent-subtle', glow: 'shadow-accent/10' },
  { label: 'Disconnected', value: stats.value.disconnected, icon: WifiOff, color: 'text-danger', bg: 'bg-danger-subtle', glow: 'shadow-danger/10' },
])
</script>

<template>
  <AppLayout>
    <div class="flex items-center gap-3">
      <Activity class="h-5 w-5 text-accent" :stroke-width="1.75" />
      <h1 class="text-gradient text-lg font-bold tracking-tight">Dashboard</h1>
    </div>

    <div v-if="error" class="mt-4 flex items-center gap-2 rounded-lg border border-danger/20 bg-danger-subtle p-3 text-[13px] text-danger">
      {{ error }}
    </div>

    <div v-else-if="loading && !data" class="mt-12 flex flex-col items-center justify-center">
      <div class="shimmer h-6 w-6 rounded-full" />
      <div class="shimmer mt-3 h-3 w-32 rounded" />
    </div>

    <template v-else>
      <!-- Stat Cards -->
      <div class="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div
          v-for="(card, i) in statCards"
          :key="card.label"
          class="card-glow stagger-item group rounded-xl border border-border-subtle bg-surface-raised p-5 transition-all duration-200 hover:shadow-lg"
          :class="card.glow"
          :style="{ animationDelay: `${i * 80}ms` }"
        >
          <div class="flex items-center justify-between">
            <p class="text-[10px] font-semibold uppercase tracking-[0.15em] text-text-muted">{{ card.label }}</p>
            <div class="flex h-8 w-8 items-center justify-center rounded-lg transition-all duration-200 group-hover:scale-110" :class="card.bg">
              <component :is="card.icon" class="h-4 w-4" :class="card.color" :stroke-width="1.75" />
            </div>
          </div>
          <p class="mt-3 text-3xl font-bold tabular-nums tracking-tight text-text-primary">{{ card.value }}</p>
        </div>
      </div>

      <!-- Recent Edges -->
      <div v-if="edges.length > 0" class="mt-8">
        <div class="flex items-center justify-between">
          <h2 class="text-[10px] font-semibold uppercase tracking-[0.15em] text-text-muted">Recent Edges</h2>
          <router-link to="/edges" class="text-[12px] font-medium text-accent transition-colors hover:text-accent-hover">
            View all &rarr;
          </router-link>
        </div>
        <div class="mt-3 space-y-1.5">
          <router-link
            v-for="(edge, i) in edges.slice(0, 5)"
            :key="edge.metadata.name"
            :to="`/edges/${edge.metadata.name}`"
            class="card-glow stagger-item group flex items-center justify-between rounded-xl border border-border-subtle bg-surface-raised px-4 py-3 transition-all duration-150 hover:bg-accent/[0.03]"
            :style="{ animationDelay: `${(i + 4) * 60}ms` }"
          >
            <div class="flex items-center gap-3">
              <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-surface-overlay">
                <Server class="h-4 w-4 text-text-muted transition-colors duration-150 group-hover:text-accent" :stroke-width="1.75" />
              </div>
              <div>
                <span class="text-[13px] font-medium text-text-primary">{{ edge.metadata.name }}</span>
                <span class="ml-2 font-mono text-[11px] text-text-muted">{{ edge.spec?.type }}</span>
              </div>
            </div>
            <div class="flex items-center gap-3">
              <StatusBadge :status="edge.status?.phase" :connected="edge.status?.connected" />
              <ChevronRight class="h-4 w-4 text-text-muted/30 transition-all duration-150 group-hover:translate-x-0.5 group-hover:text-accent/60" :stroke-width="1.75" />
            </div>
          </router-link>
        </div>
      </div>
    </template>
  </AppLayout>
</template>
