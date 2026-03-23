<script setup lang="ts">
import { computed, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useGraphQLQuery } from '@/composables/useGraphQL'
import { GET_EDGE, GET_EDGE_YAML, type GetEdgeResult, type GetEdgeYamlResult } from '@/graphql/queries/edges'
import { ArrowLeft, Server, Wifi, WifiOff, Clock, Hash, FileCode, ChevronDown, ChevronUp, Loader2 } from 'lucide-vue-next'

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
        class="flex items-center gap-1.5 rounded-lg px-2 py-1 text-[13px] text-text-muted transition-colors duration-150 hover:bg-surface-hover hover:text-text-secondary"
      >
        <ArrowLeft class="h-3.5 w-3.5" :stroke-width="1.75" />
        Edges
      </router-link>
      <span class="text-text-muted/30">/</span>
      <h1 class="text-lg font-semibold tracking-tight text-text-primary">{{ name }}</h1>
    </div>

    <div v-if="error" class="mt-4 flex items-center gap-2 rounded-lg bg-danger-subtle p-3 text-[13px] text-danger">
      {{ error }}
    </div>

    <div v-else-if="loading && !data" class="mt-12 flex flex-col items-center justify-center">
      <Loader2 class="h-6 w-6 animate-spin text-text-muted" :stroke-width="1.75" />
      <p class="mt-3 text-[13px] text-text-muted">Loading edge details...</p>
    </div>

    <template v-else-if="edge">
      <!-- Status banner -->
      <div class="mt-5 flex items-center gap-4 rounded-xl border border-border-subtle bg-surface-raised p-4">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-accent-subtle">
          <Server class="h-5 w-5 text-accent" :stroke-width="1.75" />
        </div>
        <div class="flex-1">
          <div class="flex items-center gap-3">
            <span class="text-[15px] font-semibold text-text-primary">{{ edge.metadata?.name }}</span>
            <StatusBadge :status="edge.status?.phase" :connected="edge.status?.connected" />
          </div>
          <div class="mt-0.5 flex items-center gap-1.5 text-[12px] text-text-muted">
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
        <div class="rounded-xl border border-border-subtle bg-surface-raised p-5">
          <h2 class="text-[12px] font-semibold uppercase tracking-wider text-text-muted">Details</h2>
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
        <div class="rounded-xl border border-border-subtle bg-surface-raised p-5">
          <h2 class="text-[12px] font-semibold uppercase tracking-wider text-text-muted">Conditions</h2>
          <div v-if="edge.status?.conditions?.length" class="mt-4 space-y-2">
            <div
              v-for="cond in edge.status.conditions"
              :key="cond.type"
              class="rounded-lg border border-border-subtle bg-surface-overlay p-3 transition-colors duration-150"
            >
              <div class="flex items-center justify-between">
                <span class="text-[13px] font-medium text-text-primary">{{ cond.type }}</span>
                <StatusBadge :status="cond.status === 'True' ? 'Ready' : 'Pending'" />
              </div>
              <p v-if="cond.message" class="mt-1.5 text-[12px] leading-relaxed text-text-muted">{{ cond.message }}</p>
            </div>
          </div>
          <div v-else class="mt-4 flex flex-col items-center py-6 text-text-muted/50">
            <p class="text-[13px]">No conditions</p>
          </div>
        </div>
      </div>

      <!-- YAML Toggle -->
      <div class="mt-6">
        <button
          class="flex items-center gap-2 rounded-lg border border-border-subtle bg-surface-raised px-4 py-2 text-[13px] font-medium text-text-secondary transition-all duration-150 hover:border-border-default hover:bg-surface-hover hover:text-text-primary"
          @click="showYaml = !showYaml"
        >
          <FileCode class="h-4 w-4" :stroke-width="1.75" />
          {{ showYaml ? 'Hide' : 'Show' }} YAML
          <component :is="showYaml ? ChevronUp : ChevronDown" class="h-3.5 w-3.5 text-text-muted" :stroke-width="1.75" />
        </button>

        <transition name="fade">
          <div v-if="showYaml" class="mt-3">
            <div v-if="yamlLoading" class="flex items-center gap-2 text-[13px] text-text-muted">
              <Loader2 class="h-4 w-4 animate-spin" :stroke-width="1.75" />
              Loading YAML...
            </div>
            <pre
              v-else
              class="max-h-[500px] overflow-auto rounded-xl border border-border-subtle bg-surface-overlay p-4 font-mono text-[12px] leading-relaxed text-text-secondary"
            >{{ yaml }}</pre>
          </div>
        </transition>
      </div>
    </template>
  </AppLayout>
</template>
