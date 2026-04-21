export const API_PATHS = {
  healthz: '/healthz',
  tokenLogin: '/apis/auth/token-login',
  authorize: '/apis/auth/authorize',
  graphql: (clusterName: string) => `/apis/graphql/${clusterName}`,
} as const

export const STORAGE_KEYS = {
  auth: 'kedge-auth',
} as const
