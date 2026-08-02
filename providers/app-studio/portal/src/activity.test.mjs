import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import ts from 'typescript'

const source = await readFile(new URL('./activity.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ES2022,
    target: ts.ScriptTarget.ES2022,
  },
})
const moduleURL = `data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`
const {
  assistantTaskActions,
  assistantTaskDetail,
  assistantTaskStatusLabel,
  assistantTaskViews,
} = await import(moduleURL)

const item = (overrides) => ({
  id: 'work-item-1',
  rootMessageID: 'user-1',
  createdBy: 'someone',
  status: 'suspended',
  revision: 3,
  createdAt: '2026-07-31T10:00:00Z',
  updatedAt: '2026-07-31T10:00:00Z',
  ...overrides,
})

// The rule the whole surface exists to honour: never render an action the
// backend will refuse. Cancelling requires suspended AND no live run, and a
// running task can only be stopped.
test('a running task offers only Stop', () => {
  assert.deepEqual(assistantTaskActions(item({ status: 'active', activeRunID: 'run-1' })), ['stop'])
})

test('a paused task offers Continue and Discard', () => {
  assert.deepEqual(assistantTaskActions(item({ status: 'suspended' })), ['continue', 'discard'])
})

// This is the state that blocked a real session: active with a dead run. It is
// released by reconciliation on read, and no button can clear it, so offering
// one would only produce a 409.
test('an active task whose run is gone offers no action', () => {
  assert.deepEqual(assistantTaskActions(item({ status: 'active', activeRunID: '' })), [])
  assert.match(assistantTaskDetail(item({ status: 'active', activeRunID: '' })), /reload/i)
})

test('finished tasks offer no actions', () => {
  assert.deepEqual(assistantTaskActions(item({ status: 'completed' })), [])
  assert.deepEqual(assistantTaskActions(item({ status: 'cancelled' })), [])
})

// Users of App Studio are not expected to know what a work item is.
test('status is shown in product language, not store vocabulary', () => {
  assert.equal(assistantTaskStatusLabel('active'), 'Running')
  assert.equal(assistantTaskStatusLabel('suspended'), 'Paused')
  assert.equal(assistantTaskStatusLabel('completed'), 'Done')
  assert.equal(assistantTaskStatusLabel('cancelled'), 'Discarded')
})

test('suspension reasons are explained rather than echoed', () => {
  assert.match(assistantTaskDetail(item({ statusReason: 'no_progress' })), /without making progress/i)
  assert.match(assistantTaskDetail(item({ statusReason: 'provider restarted' })), /restarted/i)
  assert.match(assistantTaskDetail(item({ statusReason: 'aborted' })), /you stopped/i)
  // An unrecognized reason still says something useful.
  assert.match(assistantTaskDetail(item({ statusReason: 'something-new' })), /paused/i)
})

test('actionable work sorts above finished work', () => {
  const views = assistantTaskViews(
    [
      item({ id: 'done', status: 'completed' }),
      item({ id: 'paused', status: 'suspended' }),
      item({ id: 'running', status: 'active', activeRunID: 'run-1' }),
    ],
    [],
  )
  assert.deepEqual(views.map((view) => view.id), ['running', 'paused', 'done'])
})

test('labels come from the message that started the task', () => {
  const views = assistantTaskViews(
    [item({ rootMessageID: 'user-7' })],
    [{ id: 'user-7', content: '  add a  swipe screen\n for jobs ' }],
  )
  assert.equal(views[0].label, 'add a swipe screen for jobs')

  const orphaned = assistantTaskViews([item({ rootMessageID: 'missing' })], [])
  assert.equal(orphaned[0].label, 'Untitled task')
})
