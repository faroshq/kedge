import { ref, watchEffect, onUnmounted, type Ref } from 'vue'
import { useAuthStore } from '@/stores/auth'

const MCP_API_BASE = (cluster: string) =>
  `/clusters/${cluster}/apis/mcp.kedge.faros.sh/v1alpha1/kubernetes`

interface UseListResult<T> {
  data: Ref<T | null>
  error: Ref<string | null>
  loading: Ref<boolean>
  refetch: () => Promise<void>
}

async function kubeRequest(
  method: string,
  path: string,
  token: string,
  body?: unknown,
): Promise<Response> {
  const opts: RequestInit = {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
  }
  if (body) opts.body = JSON.stringify(body)
  return fetch(path, opts)
}

// --- Types ---

export interface KubernetesMCP {
  apiVersion: string
  kind: string
  metadata: {
    name: string
    creationTimestamp?: string
    uid?: string
    resourceVersion?: string
    labels?: Record<string, string>
  }
  spec: {
    edgeSelector?: {
      matchLabels?: Record<string, string>
      matchExpressions?: Array<{
        key: string
        operator: string
        values?: string[]
      }>
    }
    toolsets?: string[]
    readOnly?: boolean
  }
  status?: {
    URL?: string
    connectedEdges?: number
    conditions?: Array<{
      type: string
      status: string
      reason?: string
      message?: string
      lastTransitionTime?: string
    }>
  }
}

interface KubernetesMCPList {
  items: KubernetesMCP[]
}

// --- Composables ---

export function useMCPList(pollInterval?: number): UseListResult<KubernetesMCP[]> {
  const auth = useAuthStore()
  const data = ref<KubernetesMCP[] | null>(null) as Ref<KubernetesMCP[] | null>
  const error = ref<string | null>(null)
  const loading = ref(true)
  let timer: ReturnType<typeof setInterval> | null = null

  async function execute() {
    if (!auth.clusterName) {
      error.value = 'No cluster selected'
      loading.value = false
      return
    }
    try {
      loading.value = true
      error.value = null
      const token = await auth.getValidToken()
      const resp = await kubeRequest('GET', MCP_API_BASE(auth.clusterName), token)
      if (!resp.ok) {
        error.value = `Failed to list MCP servers: ${resp.status} ${resp.statusText}`
        return
      }
      const list: KubernetesMCPList = await resp.json()
      data.value = list.items ?? []
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Request failed'
    } finally {
      loading.value = false
    }
  }

  watchEffect(() => {
    if (auth.isAuthenticated) execute()
  })

  if (pollInterval && pollInterval > 0) {
    timer = setInterval(execute, pollInterval)
  }

  onUnmounted(() => {
    if (timer) clearInterval(timer)
  })

  return { data, error, loading, refetch: execute }
}

export function useMCPGet(name: Ref<string>, pollInterval?: number): UseListResult<KubernetesMCP> {
  const auth = useAuthStore()
  const data = ref<KubernetesMCP | null>(null) as Ref<KubernetesMCP | null>
  const error = ref<string | null>(null)
  const loading = ref(true)
  let timer: ReturnType<typeof setInterval> | null = null

  async function execute() {
    if (!auth.clusterName || !name.value) {
      error.value = 'No cluster or name'
      loading.value = false
      return
    }
    try {
      loading.value = true
      error.value = null
      const token = await auth.getValidToken()
      const resp = await kubeRequest('GET', `${MCP_API_BASE(auth.clusterName)}/${name.value}`, token)
      if (!resp.ok) {
        error.value = `Failed to get MCP server: ${resp.status} ${resp.statusText}`
        return
      }
      data.value = await resp.json()
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Request failed'
    } finally {
      loading.value = false
    }
  }

  watchEffect(() => {
    if (auth.isAuthenticated && name.value) execute()
  })

  if (pollInterval && pollInterval > 0) {
    timer = setInterval(execute, pollInterval)
  }

  onUnmounted(() => {
    if (timer) clearInterval(timer)
  })

  return { data, error, loading, refetch: execute }
}

export async function createMCP(spec: KubernetesMCP['spec'], name: string): Promise<KubernetesMCP> {
  const auth = useAuthStore()
  if (!auth.clusterName) throw new Error('No cluster selected')
  const token = await auth.getValidToken()
  const body = {
    apiVersion: 'mcp.kedge.faros.sh/v1alpha1',
    kind: 'Kubernetes',
    metadata: { name },
    spec,
  }
  const resp = await kubeRequest('POST', MCP_API_BASE(auth.clusterName), token, body)
  if (!resp.ok) {
    const text = await resp.text()
    throw new Error(`Create failed: ${resp.status} ${text}`)
  }
  return resp.json()
}

export async function updateMCP(resource: KubernetesMCP): Promise<KubernetesMCP> {
  const auth = useAuthStore()
  if (!auth.clusterName) throw new Error('No cluster selected')
  const token = await auth.getValidToken()
  const resp = await kubeRequest(
    'PUT',
    `${MCP_API_BASE(auth.clusterName)}/${resource.metadata.name}`,
    token,
    resource,
  )
  if (!resp.ok) {
    const text = await resp.text()
    throw new Error(`Update failed: ${resp.status} ${text}`)
  }
  return resp.json()
}

export async function deleteMCP(name: string): Promise<void> {
  const auth = useAuthStore()
  if (!auth.clusterName) throw new Error('No cluster selected')
  const token = await auth.getValidToken()
  const resp = await kubeRequest('DELETE', `${MCP_API_BASE(auth.clusterName)}/${name}`, token)
  if (!resp.ok) {
    const text = await resp.text()
    throw new Error(`Delete failed: ${resp.status} ${text}`)
  }
}
