import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import ts from 'typescript'

const source = await readFile(new URL('./previewState.ts', import.meta.url), 'utf8')
const appSource = await readFile(new URL('./App.vue', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ES2022,
    target: ts.ScriptTarget.ES2022,
  },
})
const moduleURL = `data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`
const {
  developmentPreviewDisplayPhase,
  developmentPreviewSyncStatus,
  developmentPreviewFrameProblemMessage,
  developmentPreviewFrameStateForLoads,
  recentDevelopmentPreviewLoads,
  developmentPreviewAuthLoopThreshold,
  developmentPreviewAuthLoopWindowMS,
} = await import(moduleURL)

test('reports synced files without claiming preview refresh when route binding is missing', () => {
  assert.equal(
    developmentPreviewSyncStatus({
      hasPreviewRouteBinding: false,
      previewURL: '',
      readinessMessage: '',
      authorizationError: '',
    }, 'Synced and refreshed preview'),
    'Synced project files. Preview routing is not configured yet.',
  )
})

test('development preview uses the folded sandbox runner binding', () => {
  assert.equal(
    appSource.includes("PREVIEW_ROUTE_BINDING_NAME = 'preview-route'"),
    false,
    'preview should no longer require a separate preview-route binding',
  )
  assert.match(
    appSource,
    /const developmentPreviewRawURL = computed\(\(\) => \{\s*return projectBindingPreviewURL\(developmentBinding\.value\)\s*\}\)/,
  )
  assert.match(
    appSource,
    /const developmentPreviewNeedsAuthorization = computed\(\(\) => \{\s*return !!developmentBinding\.value && developmentBinding\.value\.provider === 'app-studio'\s*\}\)/,
  )
})

test('reports refreshed preview only after authorization returns a preview URL', () => {
  assert.equal(
    developmentPreviewSyncStatus({
      hasPreviewRouteBinding: true,
      previewURL: 'https://preview.example.com/',
      readinessMessage: '',
      authorizationError: '',
    }, 'Synced and refreshed preview'),
    'Synced and refreshed preview',
  )
})

test('keeps readiness detail when sync succeeds before preview is ready', () => {
  assert.equal(
    developmentPreviewSyncStatus({
      hasPreviewRouteBinding: true,
      previewURL: '',
      readinessMessage: 'Preview is getting ready.',
      authorizationError: '',
    }, 'Synced and refreshed preview'),
    'Synced project files. Preview is getting ready.',
  )
})

test('keeps preview badge pending when runner is ready but preview route is missing', () => {
  assert.equal(
    developmentPreviewDisplayPhase({
      previewURL: '',
      authorizationError: '',
    }),
    'Pending',
  )
})

test('marks preview badge ready only when an authorized preview URL exists', () => {
  assert.equal(
    developmentPreviewDisplayPhase({
      previewURL: 'https://preview.example.com/',
      authorizationError: '',
    }),
    'Ready',
  )
})

test('marks preview badge error when authorization failed', () => {
  assert.equal(
    developmentPreviewDisplayPhase({
      previewURL: '',
      authorizationError: 'preview authorization failed',
    }),
    'Error',
  )
})

test('a single frame load is a healthy preview', () => {
  assert.equal(developmentPreviewFrameStateForLoads([1_000]), 'loaded')
  assert.equal(developmentPreviewFrameProblemMessage('loaded'), '')
})

test('repeated loads in one window are reported as a blocked-cookie redirect loop', () => {
  const loads = Array.from({ length: developmentPreviewAuthLoopThreshold }, (_, i) => 1_000 + i * 100)
  assert.equal(developmentPreviewFrameStateForLoads(loads), 'auth_loop')
  const message = developmentPreviewFrameProblemMessage('auth_loop')
  assert.match(message, /cookie/i)
  assert.match(message, /browser tab/i)
})

test('loads outside the detection window do not accumulate into a false loop', () => {
  const now = 100_000
  const stale = [now - developmentPreviewAuthLoopWindowMS - 1, now - developmentPreviewAuthLoopWindowMS - 2]
  const recent = recentDevelopmentPreviewLoads([...stale, now - 100], now)
  assert.deepEqual(recent, [now - 100])
  assert.equal(developmentPreviewFrameStateForLoads(recent.concat(now)), 'loaded')
})

test('a frame that never loads is reported rather than left blank behind a Ready badge', () => {
  const message = developmentPreviewFrameProblemMessage('timeout')
  assert.match(message, /not finished loading/i)
  // Still-pending is not yet a problem worth showing.
  assert.equal(developmentPreviewFrameProblemMessage('pending'), '')
})
