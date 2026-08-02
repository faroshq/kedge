export interface DevelopmentPreviewSyncState {
  hasPreviewRouteBinding: boolean
  previewURL: string
  readinessMessage: string
  authorizationError: string
}

export interface DevelopmentPreviewDisplayPhaseState {
  previewURL: string
  authorizationError: string
}

export function developmentPreviewDisplayPhase(state: DevelopmentPreviewDisplayPhaseState): string {
  if (state.authorizationError) return 'Error'
  if (state.previewURL) return 'Ready'
  return 'Pending'
}

export function developmentPreviewSyncStatus(state: DevelopmentPreviewSyncState, refreshedStatus: string): string {
  if (state.previewURL && !state.authorizationError) return refreshedStatus
  if (!state.hasPreviewRouteBinding) return 'Synced project files. Preview routing is not configured yet.'
  if (state.authorizationError) return 'Synced project files. Preview authorization failed.'
  if (state.readinessMessage) return `Synced project files. ${state.readinessMessage}`
  return 'Synced project files. Preview is getting ready.'
}

/**
 * Health of the embedded preview frame, as far as it can be observed from the
 * portal.
 *
 * The preview is a separate origin, so its contents are unreadable from here.
 * That left two real failure modes invisible: a frame that never paints, and a
 * frame silently looping through an auth redirect because its session cookie
 * was blocked as a third-party cookie inside the iframe. Both showed as a blank
 * panel with a "Ready" badge.
 */
export type DevelopmentPreviewFrameState = 'pending' | 'loaded' | 'timeout' | 'auth_loop'

/** Repeated loads inside this window are treated as a redirect loop. */
export const developmentPreviewAuthLoopWindowMS = 10_000

/** Loads within the window before the frame is called a redirect loop. */
export const developmentPreviewAuthLoopThreshold = 4

/**
 * Classifies a frame load given the timestamps of recent loads (including this
 * one, newest last).
 */
export function developmentPreviewFrameStateForLoads(loadTimestamps: number[]): DevelopmentPreviewFrameState {
  return loadTimestamps.length >= developmentPreviewAuthLoopThreshold ? 'auth_loop' : 'loaded'
}

/** Drops load timestamps that fell out of the detection window. */
export function recentDevelopmentPreviewLoads(loadTimestamps: number[], now: number): number[] {
  return loadTimestamps.filter((at) => now - at < developmentPreviewAuthLoopWindowMS)
}

/**
 * The user-facing explanation for a frame that is not working, or '' when the
 * frame is fine or still legitimately loading.
 */
export function developmentPreviewFrameProblemMessage(state: DevelopmentPreviewFrameState): string {
  switch (state) {
    case 'timeout':
      return 'The preview has not finished loading. The app may still be starting, or it may not be serving on the port its template expects.'
    case 'auth_loop':
      return 'The preview keeps reloading without settling, which usually means the browser is blocking its session cookie inside this embedded frame. Open it in a browser tab instead.'
    default:
      return ''
  }
}
