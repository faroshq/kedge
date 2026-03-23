<script setup lang="ts">
import { computed, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useGraphQLQuery } from '@/composables/useGraphQL'
import { GET_EDGE, GET_EDGE_YAML, type GetEdgeResult, type GetEdgeYamlResult } from '@/graphql/queries/edges'
import { Server, Wifi, WifiOff, Clock, Hash, FileCode, ChevronDown, ChevronUp, ArrowLeft, TerminalSquare } from 'lucide-vue-next'

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

const canSSH = computed(() => {
  if (!edge.value) return false
  return edge.value.spec?.type === 'server' && edge.value.status?.connected
})

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
    <!-- Back link -->
    <router-link
      to="/edges"
      class="stagger-item mb-5 inline-flex items-center gap-1.5 rounded-lg px-2 py-1 text-[12px] text-text-muted transition-all hover:bg-surface-hover hover:text-accent"
      style="animation-delay: 0ms"
    >
      <ArrowLeft class="h-3 w-3" :stroke-width="2" />
      Back to edges
    </router-link>

    <div v-if="error" class="flex items-center gap-2 rounded-xl border border-danger/20 bg-danger-subtle p-4 text-[13px] text-danger">
      {{ error }}
    </div>

    <div v-else-if="loading && !data" class="mt-16 flex flex-col items-center justify-center gap-3">
      <div class="shimmer h-8 w-8 rounded-xl" />
      <div class="shimmer h-3 w-40 rounded" />
    </div>

    <template v-else-if="edge">
      <!-- Asymmetric layout: big hero left, stacked info right -->
      <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <!-- Hero card (spans 2 cols) -->
        <div
          class="border-beam stagger-item col-span-1 flex flex-col rounded-2xl border border-border-subtle bg-surface-raised/80 p-6 backdrop-blur lg:col-span-2"
          style="animation-delay: 40ms"
        >
          <div class="flex items-start gap-4">
            <div class="relative flex h-14 w-14 shrink-0 items-center justify-center">
              <div class="absolute inset-0 rounded-xl bg-accent/15 blur-md" />
              <div class="relative flex h-14 w-14 items-center justify-center rounded-xl border border-accent/20 bg-surface-overlay">
                <Server class="h-7 w-7 text-accent" :stroke-width="1.5" />
              </div>
            </div>
            <div class="flex-1">
              <div class="flex items-center gap-3">
                <h1 class="text-gradient text-xl font-bold tracking-tight">{{ edge.metadata?.name }}</h1>
                <StatusBadge :status="edge.status?.phase" :connected="edge.status?.connected" />
              </div>
              <div class="mt-1.5 flex items-center gap-4 text-[12px] text-text-muted">
                <div class="flex items-center gap-1.5">
                  <component
                    :is="edge.status?.connected ? Wifi : WifiOff"
                    class="h-3 w-3"
                    :class="edge.status?.connected ? 'text-success' : 'text-danger'"
                    :stroke-width="1.75"
                  />
                  {{ edge.status?.connected ? 'Connected' : 'Disconnected' }}
                </div>
                <span class="font-mono text-[11px] text-text-muted/60">{{ edge.spec?.type }}</span>
              </div>
            </div>
          </div>

          <!-- Details grid inside hero -->
          <dl class="mt-6 grid grid-cols-2 gap-x-8 gap-y-3 border-t border-border-subtle pt-5">
            <div
              v-for="item in details"
              :key="item.label"
              class="flex items-center justify-between"
            >
              <dt class="flex items-center gap-2 text-[12px] text-text-muted">
                <component :is="item.icon" class="h-3 w-3" :stroke-width="1.75" />
                {{ item.label }}
              </dt>
              <dd
                class="text-[12px] text-text-secondary"
                :class="{ 'font-mono text-[11px]': item.mono, 'max-w-[160px] truncate': item.mono }"
              >
                {{ item.value }}
              </dd>
            </div>
          </dl>
        </div>

        <!-- Conditions (right column, tall) -->
        <div
          class="stagger-item rounded-2xl border border-border-subtle bg-surface-raised/80 p-5 backdrop-blur"
          style="animation-delay: 120ms"
        >
          <h2 class="text-[10px] font-semibold uppercase tracking-[0.15em] text-text-muted">Conditions</h2>
          <div v-if="edge.status?.conditions?.length" class="mt-4 space-y-2">
            <div
              v-for="cond in edge.status.conditions"
              :key="cond.type"
              class="rounded-xl border border-border-subtle bg-surface-overlay/60 p-3 transition-colors duration-150 hover:border-border-default"
            >
              <div class="flex items-center justify-between">
                <span class="text-[12px] font-medium text-text-primary">{{ cond.type }}</span>
                <StatusBadge :status="cond.status === 'True' ? 'Ready' : 'Pending'" />
              </div>
              <p v-if="cond.message" class="mt-1.5 text-[11px] leading-relaxed text-text-muted">{{ cond.message }}</p>
            </div>
          </div>
          <div v-else class="mt-8 flex flex-col items-center text-text-muted/30">
            <p class="text-[12px]">No conditions</p>
          </div>
        </div>
      </div>

      <!-- Actions -->
      <div class="stagger-item mt-5 flex items-center gap-3" style="animation-delay: 200ms">
        <!-- SSH Terminal button -->
        <router-link
          v-if="canSSH"
          :to="`/edges/${props.name}/terminal`"
          class="glow-ring flex items-center gap-2 rounded-xl border border-accent/30 bg-accent/10 px-4 py-2 text-[12px] font-medium text-accent backdrop-blur transition-all duration-150 hover:bg-accent/20 hover:shadow-lg hover:shadow-accent/10"
        >
          <TerminalSquare class="h-3.5 w-3.5" :stroke-width="1.75" />
          SSH Terminal
        </router-link>

        <button
          class="glow-ring flex items-center gap-2 rounded-xl border border-border-subtle bg-surface-raised/80 px-4 py-2 text-[12px] font-medium text-text-secondary backdrop-blur transition-all duration-150 hover:border-accent/30 hover:text-text-primary"
          @click="showYaml = !showYaml"
        >
          <FileCode class="h-3.5 w-3.5" :stroke-width="1.75" />
          {{ showYaml ? 'Hide' : 'Show' }} YAML
          <component :is="showYaml ? ChevronUp : ChevronDown" class="h-3 w-3 text-text-muted" :stroke-width="1.75" />
        </button>
      </div>

      <div v-if="showYaml" class="stagger-item mt-3" style="animation-delay: 240ms">
        <div v-if="yamlLoading" class="flex items-center gap-2 text-[12px] text-text-muted">
          <div class="shimmer h-4 w-4 rounded" />
          Loading...
        </div>
        <div v-else class="border-beam rounded-2xl">
          <pre class="max-h-[500px] overflow-auto rounded-2xl border border-border-subtle bg-surface-overlay/60 p-5 font-mono text-[11px] leading-relaxed text-text-secondary backdrop-blur">{{ yaml }}</pre>
        </div>
      </div>
    </template>
  </AppLayout>
</template>
