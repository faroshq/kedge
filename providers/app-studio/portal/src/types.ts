export interface KedgeContext {
  token?: string | null
  user?: { email?: string; sub?: string } | null
  tenant?: string | null
  theme?: 'light' | 'dark' | 'system'
  basePath?: string
  subPath?: string
}

export interface ProjectMemory {
  goals?: string[]
  requirements?: string[]
  constraints?: string[]
}

export interface ProjectMessage {
  id: string
  projectID: string
  role: 'user' | 'assistant'
  content: string
  contentEncrypted?: boolean
  contentKeyID?: string
  metadata?: Record<string, unknown>
  createdAt: string
}

export type ProjectAssistantRunStatus = 'pending_permission' | 'pending_input' | 'running' | 'stopping' | 'completed' | 'aborted' | 'failed' | 'interrupted'
export type ProjectAssistantRunMode = 'adaptive' | 'discussion' | 'new' | 'continue'
export type ProjectAssistantWorkItemStatus = 'active' | 'suspended' | 'completed' | 'cancelled'
export type ProjectAssistantApprovalMode = 'always_ask' | 'auto_approve'

export interface ProjectAssistantApprovalPreference {
  mode: ProjectAssistantApprovalMode
  updatedAt?: string
}

export interface ProjectAssistantWorkItem {
  id: string
  rootMessageID: string
  createdBy: string
  status: ProjectAssistantWorkItemStatus
  statusReason?: string
  revision: number
  activeRunID?: string
  createdAt: string
  updatedAt: string
}

export interface ProjectAssistantRun {
  id: string
  workItemID?: string
  mode?: ProjectAssistantRunMode
  approvalMode?: ProjectAssistantApprovalMode
  status: ProjectAssistantRunStatus
  // Both omitempty on the wire: absent at revision 0 / before a message is active.
  revision?: number
  activeMessageID?: string
  clientRequestID?: string
  userMessageID?: string
  requestID?: string
  createdAt?: string
  updatedAt?: string
}

export interface ProjectAssistantSnapshot {
  run: ProjectAssistantRun
  message: ProjectMessage
}

export interface ProjectAssistantRunStart {
  run: ProjectAssistantRun
  user?: ProjectMessage
  assistant: ProjectMessage
}

export interface ProjectAssistantAbortResponse {
  runID: string
  requestID: string
  status: ProjectAssistantRunStatus
  decision?: 'allow' | 'deny'
}

export interface ProjectAssistantUndoResponse {
  runID: string
  fileCount: number
  message: ProjectMessage
}

export type ProjectAssistantActionKind = 'inspect' | 'clarify' | 'edit' | 'run' | 'commit' | 'plan' | 'other'
export type ProjectAssistantActionStatus = 'running' | 'waiting' | 'succeeded' | 'skipped' | 'failed' | 'rejected'
export type ProjectAssistantActionSeverity = 'normal' | 'attention' | 'error'
export type ProjectAssistantDiagnosticCategory = 'timeout' | 'permission' | 'validation' | 'runtime' | 'provider' | 'unknown'

export interface ProjectAssistantActionDiagnostic {
  category: ProjectAssistantDiagnosticCategory
  message: string
  referenceID: string
}

export interface ProjectAssistantActionFeedItem {
  id: string
  kind: ProjectAssistantActionKind
  status: ProjectAssistantActionStatus
  title: string
  target?: string
  outcome?: string
  count?: number
  severity: ProjectAssistantActionSeverity
  groupKey?: string
  groupTitle?: string
  sequence?: number
  diagnostic?: ProjectAssistantActionDiagnostic
}

export interface ProjectAssistantUIInterruptRequest {
  interruptId: string
  kind?: 'permission' | 'follow_up'
  surfaceId?: string
  description?: string
  questions?: string[]
  status?: 'pending' | 'resolved'
  action?: {
    runId: string
    requestId: string
    assistantMessageId?: string
  }
}

export interface Project {
  name: string
  displayName: string
  description?: string
  phase?: string
  template?: string
  repository?: {
    ref: string
    name?: string
    connectionRef?: string
    htmlURL?: string
    status?: string
    message?: string
    ready?: boolean
    commits?: ProjectRepositoryCommit[]
  }
  memory?: ProjectMemory
  environments?: ProjectEnvironment[]
  createdAt: string
  updatedAt?: string
}

export interface ProjectEnvironment {
  name: string
  mode?: string
  phase?: string
  bindings?: ProjectProviderBinding[]
}

export interface ProjectProviderBinding {
  name: string
  provider?: string
  phase?: string
  url?: string
  previewURL?: string
  outputs?: Record<string, string>
}

export interface ProjectRepositoryCommit {
  name: string
  phase?: string
  branch?: string
  commitSHA?: string
  commitURL?: string
  message?: string
  fileCount?: number
  createdAt: string
  completedAt?: string
}

export interface ProjectMessagesPage {
  items: ProjectMessage[]
  nextCursor?: string
}

export interface ProjectLLMSettings {
  provider: string
  baseURL: string
  model: string
  configured: boolean
}

export interface ProviderChild {
  displayName: string
  builtinRoute: string
}

export interface ProviderItem {
  name: string
  displayName: string
  version?: string
  ready: boolean
  hasUI: boolean
  hasBackend: boolean
  iconURL?: string
  builtinRoute?: string
  children?: ProviderChild[]
  category?: string
  builtin?: boolean
}

export interface ListResponse<T> {
  items: T[]
}

// One infrastructure template that can back a development environment
// (declares development components). Served by
// GET /api/projects/development-templates.
export interface DevelopmentTemplate {
  name: string
  displayName?: string
  description?: string
  category?: string
  components: Record<string, string>
}

// One Code repository a new project can be imported from (unclaimed).
// Served by GET /api/projects/import-repositories.
export interface ImportRepository {
  ref: string
  name?: string
  connectionRef?: string
  htmlURL?: string
}

// Result of POST /api/projects/{name}/hydrate-workspace.
export interface ProjectHydrateResult {
  repositoryRef: string
  ref?: string
  commitSHA?: string
  written?: string[]
  skipped?: string[]
}

// Result of POST /api/projects/{name}/sync-development.
export interface ProjectDevelopmentSyncResult {
  // Workspace files the sync payload cannot carry (binary or oversized);
  // they are absent from the sandbox.
  skippedFiles?: string[]
}

// One launchable component's build state, from GET /api/projects/{name}/promotion.
export interface ProjectBuildComponent {
  name: string
  imageInput: string
  built: boolean
  image?: string
  digest?: string
  tag?: string
  // Commit the published image was built from ("sha-<commit>" tag pattern);
  // empty when the tag does not identify a commit.
  builtCommit?: string
}

// Deterministic build status: built | stale | incomplete | none | unsupported.
export interface ProjectBuildCheck {
  status: string
  components?: ProjectBuildComponent[]
  missing?: string[]
  note: string
  // The project's latest successful commit — what a promote should ship.
  headCommit?: string
  // Components whose newest image was built from a commit other than headCommit.
  staleComponents?: string[]
}

// Result of GET /api/projects/{name}/promotion — gates the Promote to Prod
// action and reports the live production environment.
export interface ProjectPromotionReadiness {
  template?: string
  instance?: string
  promotable: boolean
  build: ProjectBuildCheck
  production?: ProjectProviderBinding
}

// One of the four project lifecycle checkpoints (template, git, ci, production).
// state: done | pending | blocked | error.
export interface ProjectCheckpoint {
  key: string
  label: string
  state: string
  reason?: string
  remediation?: {
    kind: string // auto | manual
    tool?: string
    actionUrl?: string
    message?: string
  }
}

// Result of GET /api/projects/{name}/checkpoints.
export interface ProjectCheckpoints {
  items: ProjectCheckpoint[]
}

// Result of POST /api/projects/{name}/promote.
export interface ProjectPromoteResult {
  environment: string
  instance: string
  components?: ProjectBuildComponent[]
}
