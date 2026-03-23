<script setup lang="ts">
import { computed, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useGraphQLQuery } from '@/composables/useGraphQL'
import { GET_EDGE, GET_EDGE_YAML, type GetEdgeResult, type GetEdgeYamlResult } from '@/graphql/queries/edges'

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
</script>

<template>
  <AppLayout>
    <div class="flex items-center gap-3">
      <router-link to="/edges" class="text-sm text-gray-500 hover:text-gray-700">Edges</router-link>
      <span class="text-gray-300">/</span>
      <h1 class="text-xl font-semibold text-gray-900">{{ name }}</h1>
    </div>

    <div v-if="error" class="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</div>
    <div v-else-if="loading && !data" class="mt-4 text-sm text-gray-500">Loading...</div>

    <template v-else-if="edge">
      <div class="mt-6 grid grid-cols-1 gap-6 lg:grid-cols-2">
        <!-- Info -->
        <div class="rounded-lg border border-gray-200 bg-white p-5">
          <h2 class="text-sm font-medium text-gray-500">Details</h2>
          <dl class="mt-3 space-y-3 text-sm">
            <div class="flex justify-between">
              <dt class="text-gray-500">Type</dt>
              <dd class="text-gray-900">{{ edge.spec?.type }}</dd>
            </div>
            <div class="flex justify-between">
              <dt class="text-gray-500">Phase</dt>
              <dd><StatusBadge :status="edge.status?.phase" :connected="edge.status?.connected" /></dd>
            </div>
            <div class="flex justify-between">
              <dt class="text-gray-500">Connected</dt>
              <dd :class="edge.status?.connected ? 'text-green-600' : 'text-red-500'">
                {{ edge.status?.connected ? 'Yes' : 'No' }}
              </dd>
            </div>
            <div class="flex justify-between">
              <dt class="text-gray-500">Hostname</dt>
              <dd class="text-gray-900">{{ edge.status?.hostname || '-' }}</dd>
            </div>
            <div class="flex justify-between">
              <dt class="text-gray-500">Agent Version</dt>
              <dd class="text-gray-900">{{ edge.status?.agentVersion || '-' }}</dd>
            </div>
            <div class="flex justify-between">
              <dt class="text-gray-500">Created</dt>
              <dd class="text-gray-900">{{ edge.metadata?.creationTimestamp }}</dd>
            </div>
            <div v-if="edge.metadata?.uid" class="flex justify-between">
              <dt class="text-gray-500">UID</dt>
              <dd class="truncate text-gray-900" style="max-width: 200px">{{ edge.metadata.uid }}</dd>
            </div>
          </dl>
        </div>

        <!-- Conditions -->
        <div class="rounded-lg border border-gray-200 bg-white p-5">
          <h2 class="text-sm font-medium text-gray-500">Conditions</h2>
          <div v-if="edge.status?.conditions?.length" class="mt-3 space-y-2">
            <div
              v-for="cond in edge.status.conditions"
              :key="cond.type"
              class="rounded-md border border-gray-100 p-3 text-sm"
            >
              <div class="flex items-center justify-between">
                <span class="font-medium text-gray-900">{{ cond.type }}</span>
                <StatusBadge :status="cond.status === 'True' ? 'Ready' : 'Pending'" />
              </div>
              <p v-if="cond.message" class="mt-1 text-gray-500">{{ cond.message }}</p>
            </div>
          </div>
          <p v-else class="mt-3 text-sm text-gray-400">No conditions</p>
        </div>
      </div>

      <!-- YAML -->
      <div class="mt-6">
        <button
          class="text-sm font-medium text-gray-600 hover:text-gray-900"
          @click="showYaml = !showYaml"
        >
          {{ showYaml ? 'Hide' : 'Show' }} YAML
        </button>
        <div v-if="showYaml" class="mt-2">
          <div v-if="yamlLoading" class="text-sm text-gray-500">Loading YAML...</div>
          <pre
            v-else
            class="max-h-96 overflow-auto rounded-lg border border-gray-200 bg-gray-900 p-4 text-sm text-gray-100"
          >{{ yaml }}</pre>
        </div>
      </div>
    </template>
  </AppLayout>
</template>
