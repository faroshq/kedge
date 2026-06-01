/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Tenant store: tracks the active Organization + Workspace for the
// portal switcher, persists the selection to localStorage, exposes
// the headers the hub REST surface expects, and lazily loads the
// caller's UMI projection.
//
// The store does NOT make any choices on the user's behalf — it just
// reflects what's selected. Bootstrap on first login picks the
// personal Org + default Workspace as a sensible default; the user
// can switch at any time.

import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import { STORAGE_KEYS } from '@/lib/constants'

const STORAGE_KEY = 'kedge:portal:tenant'

interface PersistedTenant {
  orgUUID: string | null
  workspaceUUID: string | null
}

export interface OrgRow {
  uuid: string
  displayName: string
  personal: boolean
  workspaceCreation?: string
  catalogEntryCreation?: string
  createdAt?: string
  deletionRequestedAt?: string | null
}

export interface WorkspaceRow {
  uuid: string
  orgUUID: string
  displayName: string
  deletionRequestedAt?: string | null
}

function loadPersisted(): PersistedTenant {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return { orgUUID: null, workspaceUUID: null }
    const parsed = JSON.parse(raw) as PersistedTenant
    return {
      orgUUID: parsed.orgUUID ?? null,
      workspaceUUID: parsed.workspaceUUID ?? null,
    }
  } catch {
    return { orgUUID: null, workspaceUUID: null }
  }
}

function savePersisted(value: PersistedTenant) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(value))
  } catch {
    /* ignore quota / private-mode errors */
  }
}

// authHeader pulls the OIDC token out of the existing auth storage
// slot. Reads via the shared STORAGE_KEYS constant so the key stays
// in lockstep with @/auth/token + the providers store. Kept inline
// (no @/stores/auth import) to avoid an import cycle with the auth
// store, which already depends on transitive Pinia state.
function authHeader(): Record<string, string> {
  try {
    const raw = localStorage.getItem(STORAGE_KEYS.auth)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as { idToken?: string }
    if (parsed.idToken) return { Authorization: `Bearer ${parsed.idToken}` }
  } catch {
    /* ignore */
  }
  return {}
}

export const useTenantStore = defineStore('tenant', () => {
  const persisted = loadPersisted()
  const orgUUID = ref<string | null>(persisted.orgUUID)
  const workspaceUUID = ref<string | null>(persisted.workspaceUUID)

  const orgs = ref<OrgRow[]>([])
  const workspacesByOrg = ref<Record<string, WorkspaceRow[]>>({})
  const loading = ref(false)
  const error = ref<string | null>(null)

  const activeOrg = computed<OrgRow | null>(() =>
    orgUUID.value ? orgs.value.find((o) => o.uuid === orgUUID.value) ?? null : null,
  )
  const activeWorkspace = computed<WorkspaceRow | null>(() => {
    if (!orgUUID.value || !workspaceUUID.value) return null
    const wss = workspacesByOrg.value[orgUUID.value] ?? []
    return wss.find((w) => w.uuid === workspaceUUID.value) ?? null
  })

  // Whenever the selection changes, mirror to localStorage so a
  // refresh keeps the same active context.
  watch(
    [orgUUID, workspaceUUID],
    ([o, w]) => savePersisted({ orgUUID: o, workspaceUUID: w }),
  )

  // tenantHeaders is what every /api/orgs/* request needs alongside
  // the bearer token. Empty when nothing is selected so the caller
  // can decide whether the endpoint requires them.
  function tenantHeaders(): Record<string, string> {
    const h: Record<string, string> = {}
    if (orgUUID.value) h['X-Kedge-Org'] = orgUUID.value
    if (workspaceUUID.value) h['X-Kedge-Workspace'] = workspaceUUID.value
    return h
  }

  async function fetchOrgs(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const resp = await fetch('/api/orgs', {
        headers: { ...authHeader() },
      })
      if (!resp.ok) {
        error.value = `failed to list orgs: ${resp.status}`
        orgs.value = []
        return
      }
      const data = (await resp.json()) as { items: OrgRow[] }
      orgs.value = data.items ?? []
      // Default selection: prefer the personal org; else first row.
      if (!orgUUID.value && orgs.value.length > 0) {
        const personal = orgs.value.find((o) => o.personal)
        orgUUID.value = (personal ?? orgs.value[0]).uuid
      }
      // Validate the persisted selection still exists; otherwise reset.
      if (orgUUID.value && !orgs.value.find((o) => o.uuid === orgUUID.value)) {
        orgUUID.value = orgs.value[0]?.uuid ?? null
        workspaceUUID.value = null
      }
    } catch (e: unknown) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  async function fetchWorkspaces(targetOrgUUID: string): Promise<void> {
    if (!targetOrgUUID) return
    loading.value = true
    error.value = null
    try {
      const resp = await fetch(`/api/orgs/${targetOrgUUID}/workspaces`, {
        headers: { ...authHeader(), 'X-Kedge-Org': targetOrgUUID },
      })
      if (!resp.ok) {
        error.value = `failed to list workspaces: ${resp.status}`
        workspacesByOrg.value = { ...workspacesByOrg.value, [targetOrgUUID]: [] }
        return
      }
      const data = (await resp.json()) as { items: WorkspaceRow[] }
      workspacesByOrg.value = { ...workspacesByOrg.value, [targetOrgUUID]: data.items ?? [] }
      // Default workspace selection: keep current if it still exists,
      // otherwise pick the first row.
      const list = workspacesByOrg.value[targetOrgUUID] ?? []
      if (targetOrgUUID === orgUUID.value) {
        if (!workspaceUUID.value || !list.find((w) => w.uuid === workspaceUUID.value)) {
          workspaceUUID.value = list[0]?.uuid ?? null
        }
      }
    } catch (e: unknown) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  function selectOrg(uuid: string) {
    if (orgUUID.value === uuid) return
    orgUUID.value = uuid
    // Clear workspace selection on org switch so we don't carry stale
    // state from the previous org.
    workspaceUUID.value = null
    // Lazy-load workspaces if we haven't seen them for this org.
    if (!workspacesByOrg.value[uuid]) {
      void fetchWorkspaces(uuid)
    } else {
      const list = workspacesByOrg.value[uuid] ?? []
      workspaceUUID.value = list[0]?.uuid ?? null
    }
  }

  function selectWorkspace(uuid: string) {
    workspaceUUID.value = uuid
  }

  async function createOrg(displayName: string): Promise<OrgRow | null> {
    const resp = await fetch('/api/orgs', {
      method: 'POST',
      headers: { ...authHeader(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ displayName }),
    })
    if (!resp.ok) {
      error.value = `failed to create org: ${resp.status}`
      return null
    }
    const created = (await resp.json()) as OrgRow
    await fetchOrgs()
    selectOrg(created.uuid)
    return created
  }

  async function createWorkspace(targetOrgUUID: string, displayName: string): Promise<WorkspaceRow | null> {
    const resp = await fetch(`/api/orgs/${targetOrgUUID}/workspaces`, {
      method: 'POST',
      headers: {
        ...authHeader(),
        'Content-Type': 'application/json',
        'X-Kedge-Org': targetOrgUUID,
      },
      body: JSON.stringify({ displayName }),
    })
    if (!resp.ok) {
      error.value = `failed to create workspace: ${resp.status}`
      return null
    }
    const created = (await resp.json()) as WorkspaceRow
    await fetchWorkspaces(targetOrgUUID)
    if (targetOrgUUID === orgUUID.value) {
      workspaceUUID.value = created.uuid
    }
    return created
  }

  return {
    // state
    orgUUID,
    workspaceUUID,
    orgs,
    workspacesByOrg,
    loading,
    error,
    // computed
    activeOrg,
    activeWorkspace,
    // actions
    tenantHeaders,
    fetchOrgs,
    fetchWorkspaces,
    selectOrg,
    selectWorkspace,
    createOrg,
    createWorkspace,
  }
})
