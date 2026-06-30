export interface ProjectCreateReadiness {
  gitConnection: {
    ready: boolean
    connectionRef?: string
    message?: string
  }
}

const defaultGitConnectionMessage = 'You need to connect to a Git account before you can continue'

export function gitConnectionReady(readiness: ProjectCreateReadiness | null): boolean {
  return readiness?.gitConnection.ready === true
}

export function createPromptBlockedMessage(readiness: ProjectCreateReadiness | null): string {
  if (gitConnectionReady(readiness)) return ''
  return readiness?.gitConnection.message?.trim() || defaultGitConnectionMessage
}

export function canSubmitCreatePrompt(prompt: string, readiness: ProjectCreateReadiness | null): boolean {
  return prompt.trim().length > 0 && gitConnectionReady(readiness)
}
