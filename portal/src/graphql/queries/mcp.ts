export const LIST_MCP_SERVERS = `
  query ListMCPServers {
    kedge_faros_sh {
      v1alpha1 {
        KubernetesMCPs {
          items {
            metadata {
              name
              creationTimestamp
              uid
              resourceVersion
              labels
            }
            spec {
              edgeSelector {
                matchLabels
                matchExpressions {
                  key
                  operator
                  values
                }
              }
              toolsets
              readOnly
            }
            status {
              URL
              connectedEdges
              conditions {
                type
                status
                reason
                message
                lastTransitionTime
              }
            }
          }
        }
        LinuxMCPs {
          items {
            metadata {
              name
              creationTimestamp
              uid
              resourceVersion
              labels
            }
            spec {
              edgeSelector {
                matchLabels
                matchExpressions {
                  key
                  operator
                  values
                }
              }
              toolsets
              readOnly
              commandTimeoutSeconds
              maxOutputBytes
            }
            status {
              URL
              connectedEdges
              conditions {
                type
                status
                reason
                message
                lastTransitionTime
              }
            }
          }
        }
      }
    }
  }
`

export const GET_MCP_SERVER = `
  query GetMCPServer($name: String!) {
    kedge_faros_sh {
      v1alpha1 {
        KubernetesMCP(name: $name) {
          metadata {
            name
            creationTimestamp
            uid
            resourceVersion
            labels
          }
          spec {
            edgeSelector {
              matchLabels
              matchExpressions {
                key
                operator
                values
              }
            }
            toolsets
            readOnly
          }
          status {
            URL
            connectedEdges
            conditions {
              type
              status
              reason
              message
              lastTransitionTime
            }
          }
        }
      }
    }
  }
`

export const GET_LINUX_MCP_SERVER = `
  query GetLinuxMCPServer($name: String!) {
    kedge_faros_sh {
      v1alpha1 {
        LinuxMCP(name: $name) {
          metadata {
            name
            creationTimestamp
            uid
            resourceVersion
            labels
          }
          spec {
            edgeSelector {
              matchLabels
              matchExpressions {
                key
                operator
                values
              }
            }
            toolsets
            readOnly
            commandTimeoutSeconds
            maxOutputBytes
          }
          status {
            URL
            connectedEdges
            conditions {
              type
              status
              reason
              message
              lastTransitionTime
            }
          }
        }
      }
    }
  }
`

// --- Types ---

export interface MCPMatchExpression {
  key: string
  operator: string
  values?: string[]
}

export interface MCPItem {
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
      matchExpressions?: MCPMatchExpression[]
    }
    toolsets?: string[]
    readOnly?: boolean
    // LinuxMCP-only fields; present only on Linux items, omitted on kube items.
    commandTimeoutSeconds?: number
    maxOutputBytes?: number
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

// MCPKind distinguishes the two CRD-backed MCP server kinds the portal lists.
export type MCPKind = 'kubernetes' | 'linux'

export interface ListMCPResult {
  kedge_faros_sh: {
    v1alpha1: {
      KubernetesMCPs: {
        items: MCPItem[]
      }
      LinuxMCPs: {
        items: MCPItem[]
      }
    }
  }
}

export interface GetMCPResult {
  kedge_faros_sh: {
    v1alpha1: {
      KubernetesMCP: MCPItem
    }
  }
}

export interface GetLinuxMCPResult {
  kedge_faros_sh: {
    v1alpha1: {
      LinuxMCP: MCPItem
    }
  }
}
