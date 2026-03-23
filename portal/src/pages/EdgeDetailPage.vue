<script setup lang="ts">
import { computed, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useGraphQLQuery } from '@/composables/useGraphQL'
import { GET_EDGE, GET_EDGE_YAML, type GetEdgeResult, type GetEdgeYamlResult } from '@/graphql/queries/edges'
import { ArrowLeft, Server, Wifi, WifiOff, Clock, Hash, FileCode, ChevronDown, ChevronUp } from 'lucide-vue-next'

const props = defineProps<{ name: string }>()

const { data, loading, error } = useGraphQLQuery<GetEdgeResult>(
  GET_EDGE,
  { name: props.name },
  10000,
)

const showYaml = ref(false)
const { data: yamlData, loading: yamlLoading } = useGraphQLQuery<GetEdgeYamlResult>(
  GET_EDGE_YAML,
  { name: props.name },
)

const edge = computed(() => data.value?.kedge_faros_sh?.v1alpha1?.Edge)
const yaml = computed(() => yamlData.value?.kedge_faros_sh?.v1alpha1?.EdgeYaml ?? '')

const details = computed(() => {
  if (!edge.value) return []
  return [
    { label: 'Type', value: edge.value.spec?.type, icon: Server },
    { label: 'Hostname', value: edge.value.status?.hostname || '-', icon: Server },
    { label: 'Agent Version', value: edge.value.status?.agentVersion || '-', icon: Hash },
    { label: 'Created', value: edge.value.metadata?.creationTimestamp, icon: Clock },
    { label: 'UID', value: edge.value.metadata?.uid, icon: Hash, mono: true },
  ].filter((d) => d.value)
})
</script>

<template>
  <AppLayout>
    <!-- Breadcrumb -->
    <div class="flex items-center gap-3">
      <router-link
        to="/edges"
        class="flex items-center gap-1.5 rounded-lg px-2 py-1 text-[13px] text-text-muted transition-all duration-150 hover:bg-surface-hover hover:text-accent"
      >
        <ArrowLeft class="h-3.5 w-3.5" :stroke-width="1.75" />
        Edges
      </router-link>
      <span class="text-text-muted/20">/</span>
      <h1 class="text-gradient text-lg font-bold tracking-tight">{{ name }}</h1>
    </div>

    <div v-if="error" class="mt-4 flex items-center gap-2 rounded-lg border border-danger/20 bg-danger-subtle p-3 text-[13px] text-danger">
      {{ error }}
    </div>

    <div v-else-if="loading && !data" class="mt-12 flex flex-col items-center justify-center gap-3">
      <div class="shimmer h-6 w-6 rounded-full" />
      <div class="shimmer h-3 w-40 rounded" />
    </div>

    <template v-else-if="edge">
      <!-- Status banner -->
      <div class="card-glow stagger-item mt-5 flex items-center gap-4 rounded-xl border border-border-subtle bg-surface-raised p-5">
        <div class="relative flex h-12 w-12 items-center justify-center">
          <div class="absolute inset-0 rounded-xl bg-accent/15 blur-sm" />
          <div class="relative flex h-12 w-12 items-center justify-center rounded-xl border border-accent/20 bg-surface-overlay">
            <Server class="h-6 w-6 text-accent" :stroke-width="1.75" />
          </div>
        </div>
        <div class="flex-1">
          <div class="flex items-center gap-3">
            <span class="text-[15px] font-semibold text-text-primary">{{ edge.metadata?.name }}</span>
            <StatusBadge :status="edge.status?.phase" :connected="edge.status?.connected" />
          </div>
          <div class="mt-1 flex items-center gap-1.5 text-[12px] text-text-muted">
            <component
              :is="edge.status?.connected ? Wifi : WifiOff"
              class="h-3 w-3"
              :class="edge.status?.connected ? 'text-success' : 'text-danger'"
              :stroke-width="1.75"
            />
            {{ edge.status?.connected ? 'Connected' : 'Disconnected' }}
          </div>
        </div>
      </div>

      <div class="mt-6 grid grid-cols-1 gap-5 lg:grid-cols-2">
        <!-- Details -->
        <div class="card-glow stagger-item rounded-xl border border-border-subtle bg-surface-raised p-5" style="animation-delay: 80ms">
          <h2 class="text-[10px] font-semibold uppercase tracking-[0.15em] text-text-muted">Details</h2>
          <dl class="mt-4 space-y-3.5">
            <div
              v-for="item in details"
              :key="item.label"
              class="flex items-center justify-between"
            >
              <dt class="flex items-center gap-2 text-[13px] text-text-muted">
                <component :is="item.icon" class="h-3.5 w-3.5" :stroke-width="1.75" />
                {{ item.label }}
              </dt>
              <dd
                class="text-[13px] text-text-secondary"
                :class="{ 'font-mono text-[12px]': item.mono, 'max-w-[200px] truncate': item.mono }"
              >
                {{ item.value }}
              </dd>
            </div>
          </dl>
        </div>

        <!-- Conditions -->
        <div class="card-glow stagger-item rounded-xl border border-border-subtle bg-surface-raised p-5" style="animation-delay: 160ms">
          <h2 class="text-[10px] font-semibold uppercase tracking-[0.15em] text-text-muted">Conditions</h2>
          <div v-if="edge.status?.conditions?.length" class="mt-4 space-y-2">
            <div
              v-for="cond in edge.status.conditions"
              :key="cond.type"
              class="rounded-lg border border-border-subtle bg-surface-overlay p-3 transition-colors duration-150 hover:border-border-default"
            >
              <div class="flex items-center justify-between">
                <span class="text-[13px] font-medium text-text-primary">{{ cond.type }}</span>
                <StatusBadge :status="cond.status === 'True' ? 'Ready' : 'Pending'" />
              </div>
              <p v-if="cond.message" class="mt-1.5 text-[12px] leading-relaxed text-text-muted">{{ cond.message }}</p>
            </div>
          </div>
          <div v-else class="mt-4 flex flex-col items-center py-6 text-text-muted/30">
            <p class="text-[13px]">No conditions</p>
          </div>
        </div>
      </div>

      <!-- YAML Toggle -->
      <div class="stagger-item mt-6" style="animation-delay: 240ms">
        <button
          class="glow-ring flex items-center gap-2 rounded-xl border border-border-subtle bg-surface-raised px-4 py-2 text-[13px] font-medium text-text-secondary transition-all duration-150 hover:border-accent/30 hover:text-text-primary"
          @click="showYaml = !showYaml"
        >
          <FileCode class="h-4 w-4" :stroke-width="1.75" />
          {{ showYaml ? 'Hide' : 'Show' }} YAML
          <component :is="showYaml ? ChevronUp : ChevronDown" class="h-3.5 w-3.5 text-text-muted" :stroke-width="1.75" />
        </button>

        <div v-if="showYaml" class="mt-3">
          <div v-if="yamlLoading" class="flex items-center gap-2 text-[13px] text-text-muted">
            <div class="shimmer h-4 w-4 rounded" />
            Loading YAML...
          </div>
          <pre
            v-else
            class="max-h-[500px] overflow-auto rounded-xl border border-border-subtle bg-surface-overlay/80 p-4 font-mono text-[12px] leading-relaxed text-text-secondary backdrop-blur"
          >{{ yaml }}</pre>
        </div>
      </div>
    </template>
  </AppLayout>
</template>
