import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import ts from 'typescript'

const source = await readFile(new URL('./createReadiness.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ES2022,
    target: ts.ScriptTarget.ES2022,
  },
})
const moduleURL = `data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`
const {
  canSubmitCreatePrompt,
  createPromptBlockedMessage,
} = await import(moduleURL)

test('blocks project creation when no validated Git connection is ready', () => {
  const readiness = {
    gitConnection: {
      ready: false,
      message: 'You need to connect to a Git account before you can continue',
    },
  }

  assert.equal(canSubmitCreatePrompt('build a dashboard', readiness), false)
  assert.equal(
    createPromptBlockedMessage(readiness),
    'You need to connect to a Git account before you can continue',
  )
})

test('allows the a-ha prompt only after Git durability is ready', () => {
  const readiness = {
    gitConnection: {
      ready: true,
      connectionRef: 'github',
    },
  }

  assert.equal(canSubmitCreatePrompt('build a dashboard', readiness), true)
  assert.equal(createPromptBlockedMessage(readiness), '')
})

test('still requires the user to type a prompt before submitting', () => {
  const readiness = {
    gitConnection: {
      ready: true,
      connectionRef: 'github',
    },
  }

  assert.equal(canSubmitCreatePrompt('   ', readiness), false)
})
