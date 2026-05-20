<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import ResourceTable from '@/components/ResourceTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import MCPCreateModal from '@/components/MCPCreateModal.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useGraphQLQuery, graphqlMutate } from '@/composables/useGraphQL'
import { useAuthStore } from '@/stores/auth'
import { LIST_MCP_SERVERS, type ListMCPResult, type MCPItem, type MCPKind } from '@/graphql/queries/mcp'
import { DELETE_MCP, DELETE_LINUX_MCP } from '@/graphql/mutations'
import { Bot, Plus, Server, Wifi, Copy, Check, Trash2, ClipboardCopy, Terminal } from 'lucide-vue-next'

const router = useRouter()
const auth = useAuthStore()
const { data, loading, error, refetch } = useGraphQLQuery<ListMCPResult>(LIST_MCP_SERVERS, undefined, 10000)
const showCreate = ref(false)
const copiedField = ref<string | null>(null)

// MCP servers come from two CRDs:
//   - KubernetesMCPs: route to kubernetes-type edges, served at /services/mcp/...
//   - LinuxMCPs:      route to server-type edges over SSH, served at /services/linux-mcp/...
// The portal table merges both into a single list with a "Kind" column.
const kubeMCPs = computed(() => data.value?.kedge_faros_sh?.v1alpha1?.KubernetesMCPs?.items ?? [])
const linuxMCPs = computed(() => data.value?.kedge_faros_sh?.v1alpha1?.LinuxMCPs?.items ?? [])
const defaultMCP = computed(() => kubeMCPs.value.find((m) => m.metadata.name === 'default'))
const defaultLinuxMCP = computed(() => linuxMCPs.value.find((m) => m.metadata.name === 'default'))

const columns = [
  { key: 'name', label: 'Name' },
  { key: 'kind', label: 'Kind' },
  { key: 'url', label: 'Endpoint URL' },
  { key: 'connectedEdges', label: 'Connected Edges' },
  { key: 'toolsets', label: 'Toolsets' },
  { key: 'readOnly', label: 'Read Only' },
  { key: 'status', label: 'Status' },
  { key: 'actions', label: '' },
]

// MCPRow is a Record so it satisfies ResourceTable's row constraint
// (the table indexes columns by string key); the field set just happens to
// be a fixed mix of strings, numbers, and an _raw escape hatch.
interface MCPRow extends Record<string, unknown> {
  name: string
  kind: MCPKind
  url: string
  connectedEdges: number
  toolsets: string
  readOnly: string
  status: string
  _raw: MCPItem
}

function toRow(m: MCPItem, kind: MCPKind): MCPRow {
  const readyCond = m.status?.conditions?.find((c) => c.type === 'Ready')
  return {
    name: m.metadata.name,
    kind,
    url: m.status?.URL ?? '-',
    connectedEdges: m.status?.connectedEdges ?? 0,
    toolsets: m.spec?.toolsets?.length ? m.spec.toolsets.join(', ') : 'all',
    readOnly: m.spec?.readOnly ? 'Yes' : 'No',
    status: readyCond?.status === 'True' ? 'Ready' : 'Pending',
    _raw: m,
  }
}

const rows = computed<MCPRow[]>(() => [
  ...kubeMCPs.value.map((m) => toRow(m, 'kubernetes')),
  ...linuxMCPs.value.map((m) => toRow(m, 'linux')),
])

const stats = computed(() => {
  const all = [...kubeMCPs.value, ...linuxMCPs.value]
  const total = all.length
  const ready = all.filter((m) => {
    const cond = m.status?.conditions?.find((c) => c.type === 'Ready')
    return cond?.status === 'True'
  }).length
  const totalEdges = all.reduce((sum, m) => sum + (m.status?.connectedEdges ?? 0), 0)
  return { total, ready, totalEdges }
})

function handleRowClick(row: Record<string, unknown>) {
  // Linux MCP detail page TBD — for now both kinds route to the kube detail
  // page since KubernetesMCP and LinuxMCP have near-identical schemas.
  router.push(`/mcp/${row.name}?kind=${row.kind}`)
}

interface DeleteTarget {
  name: string
  kind: MCPKind
}
const deleteTarget = ref<DeleteTarget | null>(null)
const deleteBusy = ref(false)
const deleteError = ref<string | null>(null)

function requestDelete(name: string, kind: MCPKind, event: Event) {
  event.stopPropagation()
  deleteError.value = null
  deleteTarget.value = { name, kind }
}

async function confirmDelete() {
  if (!deleteTarget.value) return
  deleteBusy.value = true
  deleteError.value = null
  try {
    const mutation = deleteTarget.value.kind === 'linux' ? DELETE_LINUX_MCP : DELETE_MCP
    await graphqlMutate(mutation, { name: deleteTarget.value.name })
    deleteTarget.value = null
    await refetch()
  } catch (e) {
    deleteError.value = e instanceof Error ? e.message : 'Delete failed'
  } finally {
    deleteBusy.value = false
  }
}

function cancelDelete() {
  if (deleteBusy.value) return
  deleteTarget.value = null
  deleteError.value = null
}

function handleCreated() {
  showCreate.value = false
  refetch()
}

// --- Config snippet generation ---
const maskedToken = '••••••••••••••••'

// urlFor pulls the endpoint off the default MCP of a given kind.  Returns a
// placeholder when the controller hasn't populated status.URL yet.
function urlFor(kind: MCPKind): string {
  const item = kind === 'linux' ? defaultLinuxMCP.value : defaultMCP.value
  return item?.status?.URL ?? '<MCP_URL>'
}

// labelFor / serverNameFor produce the bits that vary by kind (Claude config
// shortname, headline label).  Keeping these tiny helpers next to the
// snippet builders so any future field stays co-located.
function serverNameFor(kind: MCPKind): string {
  return kind === 'linux' ? 'kedge-linux' : 'kedge'
}

function buildClaudeCodeSnippet(token: string, kind: MCPKind) {
  return `claude mcp add --transport http ${serverNameFor(kind)} "${urlFor(kind)}" \\
  -H "Authorization: Bearer ${token}"`
}

function buildClaudeDesktopSnippet(token: string, kind: MCPKind) {
  return JSON.stringify(
    {
      mcpServers: {
        [serverNameFor(kind)]: {
          url: urlFor(kind),
          headers: { Authorization: `Bearer ${token}` },
        },
      },
    },
    null,
    2,
  )
}

const claudeCodeSnippet = computed(() => buildClaudeCodeSnippet(maskedToken, 'kubernetes'))
const claudeDesktopSnippet = computed(() => buildClaudeDesktopSnippet(maskedToken, 'kubernetes'))
const claudeCodeSnippetLinux = computed(() => buildClaudeCodeSnippet(maskedToken, 'linux'))
const claudeDesktopSnippetLinux = computed(() => buildClaudeDesktopSnippet(maskedToken, 'linux'))

async function copySnippet(
  builder: (token: string, kind: MCPKind) => string,
  field: string,
  kind: MCPKind = 'kubernetes',
) {
  try {
    const token = await auth.getValidToken()
    await navigator.clipboard.writeText(builder(token, kind))
    copiedField.value = field
    setTimeout(() => (copiedField.value = null), 2000)
  } catch {
    // fallback
  }
}

async function copyToClipboard(text: string, field: string) {
  try {
    await navigator.clipboard.writeText(text)
    copiedField.value = field
    setTimeout(() => (copiedField.value = null), 2000)
  } catch {
    // fallback
  }
}
</script>

<template>
  <AppLayout>
    <!-- Header row -->
    <div class="stagger-item mb-5 flex items-center justify-between" style="animation-delay: 0ms">
      <div class="flex items-center gap-3">
        <div class="flex items-center gap-2 rounded-xl border border-border-subtle bg-surface-raised/80 px-3 py-2 backdrop-blur">
          <Bot class="h-3.5 w-3.5 text-accent" :stroke-width="1.75" />
          <span class="text-[20px] font-bold tabular-nums text-text-primary">{{ stats.total }}</span>
          <span class="text-[10px] font-semibold uppercase tracking-[0.12em] text-text-muted">MCP servers</span>
        </div>
        <div class="flex items-center gap-2 rounded-xl border border-border-subtle bg-surface-raised/80 px-3 py-2 backdrop-blur">
          <Wifi class="h-3.5 w-3.5 text-success" :stroke-width="1.75" />
          <span class="text-[20px] font-bold tabular-nums text-success">{{ stats.totalEdges }}</span>
          <span class="text-[10px] font-semibold uppercase tracking-[0.12em] text-text-muted">edges connected</span>
        </div>
        <div class="ml-auto flex items-center gap-1.5">
          <div class="live-dot h-1.5 w-1.5 rounded-full text-success" />
          <span class="font-mono text-[10px] text-text-muted">auto-refresh 10s</span>
        </div>
      </div>
      <button
        class="glow-ring flex items-center gap-2 rounded-xl border border-accent/30 bg-accent/10 px-3.5 py-2 text-[12px] font-medium text-accent transition-all hover:bg-accent/20"
        @click="showCreate = true"
      >
        <Plus class="h-3.5 w-3.5" :stroke-width="2" />
        New MCP Server
      </button>
    </div>

    <!-- Global Kubernetes MCP config card -->
    <div
      v-if="defaultMCP"
      class="border-beam stagger-item mb-5 rounded-2xl border border-border-subtle bg-surface-raised/80 p-6 backdrop-blur"
      style="animation-delay: 60ms"
    >
      <div class="flex items-center gap-2 mb-4">
        <Bot class="h-4 w-4 text-accent" :stroke-width="1.75" />
        <span class="text-[10px] font-semibold uppercase tracking-[0.15em] text-text-muted">Kubernetes MCP — Default</span>
        <StatusBadge
          :status="defaultMCP.status?.conditions?.find(c => c.type === 'Ready')?.status === 'True' ? 'Ready' : 'Pending'"
        />
      </div>

      <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <!-- Claude Code config -->
        <div class="rounded-xl border border-border-subtle bg-surface-overlay/60 p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[11px] font-semibold uppercase tracking-wider text-text-muted">Claude Code</span>
            <button
              class="flex items-center gap-1 rounded-md px-2 py-1 text-[10px] text-text-muted transition-all hover:bg-surface-hover hover:text-accent"
              @click="copySnippet(buildClaudeCodeSnippet, 'claude-code', 'kubernetes')"
            >
              <component :is="copiedField === 'claude-code' ? Check : Copy" class="h-3 w-3" :stroke-width="2" />
              {{ copiedField === 'claude-code' ? 'Copied' : 'Copy' }}
            </button>
          </div>
          <pre class="overflow-x-auto rounded-lg bg-surface/80 p-3 font-mono text-[11px] leading-relaxed text-text-secondary">{{ claudeCodeSnippet }}</pre>
        </div>

        <!-- Claude Desktop config -->
        <div class="rounded-xl border border-border-subtle bg-surface-overlay/60 p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[11px] font-semibold uppercase tracking-wider text-text-muted">Claude Desktop / claude_desktop_config.json</span>
            <button
              class="flex items-center gap-1 rounded-md px-2 py-1 text-[10px] text-text-muted transition-all hover:bg-surface-hover hover:text-accent"
              @click="copySnippet(buildClaudeDesktopSnippet, 'claude-desktop', 'kubernetes')"
            >
              <component :is="copiedField === 'claude-desktop' ? Check : Copy" class="h-3 w-3" :stroke-width="2" />
              {{ copiedField === 'claude-desktop' ? 'Copied' : 'Copy' }}
            </button>
          </div>
          <pre class="overflow-x-auto rounded-lg bg-surface/80 p-3 font-mono text-[11px] leading-relaxed text-text-secondary">{{ claudeDesktopSnippet }}</pre>
        </div>
      </div>

      <!-- Endpoint URL -->
      <div class="mt-4 flex items-center gap-2">
        <span class="text-[11px] text-text-muted">Endpoint:</span>
        <code class="rounded-md border border-border-subtle bg-surface-overlay px-2 py-0.5 font-mono text-[11px] text-accent">
          {{ defaultMCP.status?.URL ?? 'Pending...' }}
        </code>
        <button
          v-if="defaultMCP.status?.URL"
          class="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-text-muted transition-all hover:text-accent"
          @click="copyToClipboard(defaultMCP.status!.URL!, 'url')"
        >
          <component :is="copiedField === 'url' ? Check : ClipboardCopy" class="h-3 w-3" :stroke-width="2" />
        </button>
        <span class="text-[11px] text-text-muted">
          &middot; {{ defaultMCP.status?.connectedEdges ?? 0 }} edges
          &middot; {{ defaultMCP.spec?.toolsets?.length ? defaultMCP.spec.toolsets.join(', ') : 'all toolsets' }}
          <template v-if="defaultMCP.spec?.readOnly"> &middot; read-only</template>
        </span>
      </div>
    </div>

    <!-- Global Linux MCP config card (server-type edges over SSH) -->
    <div
      v-if="defaultLinuxMCP"
      class="border-beam stagger-item mb-5 rounded-2xl border border-border-subtle bg-surface-raised/80 p-6 backdrop-blur"
      style="animation-delay: 90ms"
    >
      <div class="flex items-center gap-2 mb-4">
        <Terminal class="h-4 w-4 text-accent" :stroke-width="1.75" />
        <span class="text-[10px] font-semibold uppercase tracking-[0.15em] text-text-muted">Linux MCP — Default (SSH)</span>
        <StatusBadge
          :status="defaultLinuxMCP.status?.conditions?.find(c => c.type === 'Ready')?.status === 'True' ? 'Ready' : 'Pending'"
        />
      </div>

      <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <!-- Claude Code config -->
        <div class="rounded-xl border border-border-subtle bg-surface-overlay/60 p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[11px] font-semibold uppercase tracking-wider text-text-muted">Claude Code</span>
            <button
              class="flex items-center gap-1 rounded-md px-2 py-1 text-[10px] text-text-muted transition-all hover:bg-surface-hover hover:text-accent"
              @click="copySnippet(buildClaudeCodeSnippet, 'claude-code-linux', 'linux')"
            >
              <component :is="copiedField === 'claude-code-linux' ? Check : Copy" class="h-3 w-3" :stroke-width="2" />
              {{ copiedField === 'claude-code-linux' ? 'Copied' : 'Copy' }}
            </button>
          </div>
          <pre class="overflow-x-auto rounded-lg bg-surface/80 p-3 font-mono text-[11px] leading-relaxed text-text-secondary">{{ claudeCodeSnippetLinux }}</pre>
        </div>

        <!-- Claude Desktop config -->
        <div class="rounded-xl border border-border-subtle bg-surface-overlay/60 p-4">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[11px] font-semibold uppercase tracking-wider text-text-muted">Claude Desktop / claude_desktop_config.json</span>
            <button
              class="flex items-center gap-1 rounded-md px-2 py-1 text-[10px] text-text-muted transition-all hover:bg-surface-hover hover:text-accent"
              @click="copySnippet(buildClaudeDesktopSnippet, 'claude-desktop-linux', 'linux')"
            >
              <component :is="copiedField === 'claude-desktop-linux' ? Check : Copy" class="h-3 w-3" :stroke-width="2" />
              {{ copiedField === 'claude-desktop-linux' ? 'Copied' : 'Copy' }}
            </button>
          </div>
          <pre class="overflow-x-auto rounded-lg bg-surface/80 p-3 font-mono text-[11px] leading-relaxed text-text-secondary">{{ claudeDesktopSnippetLinux }}</pre>
        </div>
      </div>

      <!-- Endpoint URL -->
      <div class="mt-4 flex items-center gap-2">
        <span class="text-[11px] text-text-muted">Endpoint:</span>
        <code class="rounded-md border border-border-subtle bg-surface-overlay px-2 py-0.5 font-mono text-[11px] text-accent">
          {{ defaultLinuxMCP.status?.URL ?? 'Pending...' }}
        </code>
        <button
          v-if="defaultLinuxMCP.status?.URL"
          class="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-text-muted transition-all hover:text-accent"
          @click="copyToClipboard(defaultLinuxMCP.status!.URL!, 'url-linux')"
        >
          <component :is="copiedField === 'url-linux' ? Check : ClipboardCopy" class="h-3 w-3" :stroke-width="2" />
        </button>
        <span class="text-[11px] text-text-muted">
          &middot; {{ defaultLinuxMCP.status?.connectedEdges ?? 0 }} edges
          &middot; {{ defaultLinuxMCP.spec?.toolsets?.length ? defaultLinuxMCP.spec.toolsets.join(', ') : 'core toolset' }}
          <template v-if="defaultLinuxMCP.spec?.readOnly"> &middot; read-only</template>
        </span>
      </div>
    </div>

    <!-- Table -->
    <div class="border-beam stagger-item rounded-2xl" style="animation-delay: 120ms">
      <ResourceTable
        :columns="columns"
        :rows="rows"
        :loading="loading && !data"
        :error="error"
        @row-click="handleRowClick"
      >
        <template #name="{ value }">
          <div class="flex items-center gap-2">
            <span class="font-medium text-text-primary">{{ value }}</span>
            <span
              v-if="value === 'default'"
              class="rounded-full border border-accent/20 bg-accent-subtle px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wider text-accent"
            >
              global
            </span>
          </div>
        </template>
        <template #kind="{ value }">
          <span
            class="inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wider"
            :class="value === 'linux'
              ? 'border-warning/30 bg-warning/10 text-warning'
              : 'border-accent/30 bg-accent/10 text-accent'"
          >
            <component :is="value === 'linux' ? Terminal : Bot" class="h-3 w-3" :stroke-width="2" />
            {{ value === 'linux' ? 'Linux' : 'Kubernetes' }}
          </span>
        </template>
        <template #url="{ value }">
          <span class="font-mono text-[11px] text-text-muted max-w-[300px] truncate block">{{ value }}</span>
        </template>
        <template #connectedEdges="{ value }">
          <div class="flex items-center gap-1.5">
            <Server class="h-3 w-3 text-text-muted" :stroke-width="1.75" />
            <span class="text-[13px] font-medium" :class="(value as number) > 0 ? 'text-success' : 'text-text-muted'">{{ value }}</span>
          </div>
        </template>
        <template #toolsets="{ value }">
          <span class="rounded-md border border-border-subtle bg-surface-overlay px-2 py-0.5 font-mono text-[11px] text-text-secondary">{{ value }}</span>
        </template>
        <template #status="{ value }">
          <StatusBadge :status="value as string" />
        </template>
        <template #readOnly="{ value }">
          <span class="text-[12px]" :class="value === 'Yes' ? 'text-warning' : 'text-text-muted'">{{ value }}</span>
        </template>
        <template #actions="{ row }">
          <button
            v-if="row.name !== 'default'"
            class="flex h-7 w-7 items-center justify-center rounded-lg text-text-muted/40 opacity-0 transition-all group-hover:opacity-100 hover:bg-danger-subtle hover:text-danger"
            title="Delete MCP server"
            @click.stop="requestDelete(row.name as string, row.kind as MCPKind, $event)"
          >
            <Trash2 class="h-3.5 w-3.5" :stroke-width="1.75" />
          </button>
        </template>
      </ResourceTable>
    </div>

    <!-- Create modal -->
    <MCPCreateModal
      v-if="showCreate"
      @close="showCreate = false"
      @created="handleCreated"
    />

    <!-- Delete confirmation -->
    <ConfirmDialog
      v-if="deleteTarget"
      title="Delete MCP server?"
      :message="`This will permanently delete ${deleteTarget.kind === 'linux' ? 'Linux' : 'Kubernetes'} MCP server '${deleteTarget.name}'. This cannot be undone.`"
      confirm-label="Delete"
      :busy="deleteBusy"
      @cancel="cancelDelete"
      @confirm="confirmDelete"
    />
    <div
      v-if="deleteError"
      class="fixed bottom-4 right-4 z-[110] rounded-lg border border-danger/20 bg-danger-subtle px-4 py-3 text-[12px] text-danger shadow-lg"
    >
      {{ deleteError }}
    </div>
  </AppLayout>
</template>
