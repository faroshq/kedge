<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { RefreshCw, Trash2, Plus, Boxes, ChevronRight, ChevronDown, Save, KeyRound } from 'lucide-vue-next'
import {
  listServices, createKubeEdgeService, deleteEdgeService,
  updateEdgeServiceInstructions, connectEdgeService, listEdges,
} from './api'
import type { EdgeService, EdgeServiceDraft, Edge, ErrorResponse } from './types'

const services = ref<EdgeService[]>([])
const edges = ref<Edge[]>([])
const loading = ref(true)
const error = ref<string | null>(null)

const showCreate = ref(false)
const busy = ref(false)
const draft = ref<EdgeServiceDraft>({
  name: '',
  edgeName: '',
  serviceType: 'home-assistant',
  targetNamespace: '',
  targetName: '',
  port: 8123,
  instructions: '',
})

// Per-row expand for edit (instructions) + connect (token).
const expanded = ref<string | null>(null)
const editInstructions = ref('')
const editToken = ref('')
function toggle(s: EdgeService) {
  if (expanded.value === s.name) {
    expanded.value = null
    return
  }
  expanded.value = s.name
  editInstructions.value = s.instructions ?? ''
  editToken.value = ''
}

async function refresh() {
  loading.value = true
  error.value = null
  try {
    ;[services.value, edges.value] = await Promise.all([listServices(), listEdges()])
    if (!draft.value.edgeName && edges.value.length) draft.value.edgeName = edges.value[0].name
  } catch (e) {
    error.value = (e as ErrorResponse)?.message ?? 'Failed to load services'
  } finally {
    loading.value = false
  }
}

async function onCreate() {
  if (!draft.value.name.trim() || !draft.value.edgeName || !draft.value.targetName.trim()) return
  busy.value = true
  error.value = null
  try {
    await createKubeEdgeService({
      name: draft.value.name.trim(),
      edgeName: draft.value.edgeName,
      serviceType: draft.value.serviceType,
      targetNamespace: draft.value.targetNamespace.trim() || 'default',
      targetName: draft.value.targetName.trim(),
      port: Number(draft.value.port) || 8123,
      instructions: draft.value.instructions?.trim() || undefined,
    })
    showCreate.value = false
    draft.value = { name: '', edgeName: edges.value[0]?.name ?? '', serviceType: 'home-assistant', targetNamespace: '', targetName: '', port: 8123, instructions: '' }
    await refresh()
  } catch (e) {
    error.value = (e as ErrorResponse)?.message ?? 'Create failed'
  } finally {
    busy.value = false
  }
}

async function onDelete(s: EdgeService) {
  if (!confirm(`Delete service "${s.name}"? Its MCP tools stop being exposed.`)) return
  try {
    await deleteEdgeService(s.name)
    await refresh()
  } catch (e) {
    error.value = (e as ErrorResponse)?.message ?? 'Delete failed'
  }
}

async function onSaveInstructions(s: EdgeService) {
  busy.value = true
  error.value = null
  try {
    await updateEdgeServiceInstructions(s.name, editInstructions.value)
    await refresh()
  } catch (e) {
    error.value = (e as ErrorResponse)?.message ?? 'Save failed'
  } finally {
    busy.value = false
  }
}

async function onConnect(s: EdgeService) {
  if (!editToken.value.trim()) return
  busy.value = true
  error.value = null
  try {
    await connectEdgeService(s.name, editToken.value.trim())
    editToken.value = ''
    await refresh()
  } catch (e) {
    error.value = (e as ErrorResponse)?.message ?? 'Connect failed'
  } finally {
    busy.value = false
  }
}

onMounted(refresh)
const timer = setInterval(refresh, 10000)
onUnmounted(() => clearInterval(timer))

function phaseClass(p?: string): string {
  return p === 'Ready' ? 'ok' : p === 'Unreachable' ? 'down' : 'pending'
}
</script>

<template>
  <div class="edges-app">
    <header class="edges-header">
      <div>
        <h1>Services</h1>
        <p>Services running next to your edges (e.g. Home Assistant). Attach a token to make one Ready, and give it AI guidance — its tools appear in the MCP endpoint.</p>
      </div>
      <div class="header-actions">
        <button class="btn" :disabled="loading" @click="refresh">
          <RefreshCw :size="14" :class="{ spin: loading }" /> Refresh
        </button>
        <button class="btn primary" @click="showCreate = !showCreate">
          <Plus :size="14" /> New service
        </button>
      </div>
    </header>

    <div v-if="error" class="banner error">{{ error }}</div>

    <!-- Create form -->
    <div v-if="showCreate" class="wiz-card" style="margin-bottom: 16px;">
      <h3>New service</h3>
      <div class="row" style="gap: 12px; align-items: flex-start;">
        <label class="fld" style="flex: 1;">
          <span class="lbl">Name</span>
          <input v-model="draft.name" class="input" placeholder="ha" />
        </label>
        <label class="fld" style="flex: 1;">
          <span class="lbl">Edge (KubernetesCluster)</span>
          <select v-model="draft.edgeName" class="input">
            <option v-for="e in edges" :key="e.name" :value="e.name">{{ e.name }}</option>
          </select>
        </label>
      </div>
      <div class="row" style="gap: 12px; align-items: flex-start;">
        <label class="fld" style="flex: 1;">
          <span class="lbl">Type</span>
          <input v-model="draft.serviceType" class="input" placeholder="home-assistant" />
        </label>
        <label class="fld" style="flex: 1;">
          <span class="lbl">Port</span>
          <input v-model="draft.port" type="number" min="1" max="65535" class="input" />
        </label>
      </div>
      <div class="row" style="gap: 12px; align-items: flex-start;">
        <label class="fld" style="flex: 1;">
          <span class="lbl">Target namespace</span>
          <input v-model="draft.targetNamespace" class="input" placeholder="home" />
        </label>
        <label class="fld" style="flex: 1;">
          <span class="lbl">Target service name</span>
          <input v-model="draft.targetName" class="input" placeholder="home-assistant" />
        </label>
      </div>
      <label class="fld">
        <span class="lbl">AI instructions (optional)</span>
        <textarea v-model="draft.instructions" class="input" rows="3" placeholder="Gates are cover.gate_main. Living room light is light.living_room."></textarea>
      </label>
      <div class="wiz-actions">
        <button class="btn" @click="showCreate = false">Cancel</button>
        <button class="btn primary" :disabled="busy || !draft.name.trim() || !draft.edgeName || !draft.targetName.trim()" @click="onCreate">Create</button>
      </div>
    </div>

    <div v-if="loading && services.length === 0" class="muted pad">Loading services…</div>

    <div v-else-if="services.length === 0" class="empty">
      <Boxes :size="28" />
      <div class="empty-title">No services yet</div>
      <div class="muted">Click <b>New service</b> to declare one (e.g. Home Assistant) on a Kubernetes edge.</div>
    </div>

    <div v-else class="edges-table-wrap">
      <table class="edges-table">
        <thead>
          <tr>
            <th></th>
            <th>Name</th>
            <th>Edge</th>
            <th>Type</th>
            <th>Target</th>
            <th>Status</th>
            <th>Token</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <template v-for="s in services" :key="s.name">
            <tr class="clickable" @click="toggle(s)">
              <td><component :is="expanded === s.name ? ChevronDown : ChevronRight" :size="14" /></td>
              <td class="name">{{ s.name }}</td>
              <td class="muted">{{ s.edgeName || '—' }}</td>
              <td class="mono muted">{{ s.serviceType || '—' }}</td>
              <td class="mono muted">{{ s.targetNamespace ? s.targetNamespace + '/' : '' }}{{ s.targetName || '—' }}:{{ s.port || '' }}</td>
              <td><span class="status" :class="phaseClass(s.phase)">{{ s.phase || 'Pending' }}</span></td>
              <td>{{ s.hasCredentials ? '✓' : '—' }}</td>
              <td class="actions">
                <button class="icon danger" title="Delete" @click.stop="onDelete(s)"><Trash2 :size="14" /></button>
              </td>
            </tr>
            <tr v-if="expanded === s.name" class="detail-row">
              <td colspan="8">
                <div class="es-head">AI instructions</div>
                <textarea v-model="editInstructions" class="input" rows="4" placeholder="Describe this service's entities/rooms so the AI knows your setup."></textarea>
                <div class="wiz-actions" style="margin: 8px 0 16px;">
                  <button class="btn primary" :disabled="busy" @click="onSaveInstructions(s)"><Save :size="14" /> Save instructions</button>
                </div>

                <div class="es-head">Access token</div>
                <div class="muted" style="margin-bottom: 6px;">Paste a long-lived token (e.g. Home Assistant) to make the service Ready. Stored as a Secret, never on the agent host.</div>
                <div class="row" style="gap: 8px;">
                  <input v-model="editToken" type="password" class="input" style="flex: 1;" placeholder="token" />
                  <button class="btn" :disabled="busy || !editToken.trim()" @click="onConnect(s)"><KeyRound :size="14" /> {{ s.hasCredentials ? 'Update token' : 'Set token' }}</button>
                </div>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </div>
</template>
