import type { WorkspaceRow } from '@/stores/tenant'

/**
 * Extract cluster name from kubeconfig server URL.
 * Reuses the existing regex pattern from auth/token.ts.
 */
export function extractClusterName(kubeconfig: string): string {
  const match = kubeconfig.match(/\/clusters\/([^\s/"]+)/)
  return match?.[1] ?? ''
}

/**
 * Generate a workspace-specific kubeconfig by replacing the cluster name
 * in the server URL.
 */
export function generateWorkspaceKubeconfig(baseKubeconfig: string, workspaceClusterName: string): string {
  if (!workspaceClusterName) return baseKubeconfig
  const oldCluster = extractClusterName(baseKubeconfig)
  if (!oldCluster || oldCluster === workspaceClusterName) return baseKubeconfig

  // Replace the cluster name in the server URL line only.
  const escapedOld = oldCluster.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const serverRegex = new RegExp(`^(\\s*server:\\s*.*?/clusters/)${escapedOld}([\\s/"]*)$`, 'gm')
  return baseKubeconfig.replace(serverRegex, `$1${workspaceClusterName}$2`)
}

/**
 * Trigger a file download in the browser.
 */
export function downloadKubeconfig(yaml: string, filename: string): void {
  const blob = new Blob([yaml], { type: 'text/yaml' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

/**
 * Compute a safe filename for a workspace kubeconfig.
 */
export function kubeconfigFilename(workspace: WorkspaceRow): string {
  const base = workspace.displayName?.trim() || workspace.uuid
  // Sanitize: replace filesystem-unfriendly chars with hyphens
  const safe = base.replace(/[^a-zA-Z0-9._-]/g, '-').replace(/-+/g, '-').replace(/^-|-$/g, '')
  return `kedge-${safe}.kubeconfig`
}