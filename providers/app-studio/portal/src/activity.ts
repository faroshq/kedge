import type { ProjectAssistantWorkItem, ProjectAssistantWorkItemStatus } from './types'

/**
 * View model for the Activity tab.
 *
 * A project runs one assistant task at a time, and until this surface existed
 * that fact was invisible: a running turn could only be seen inside the
 * conversation you happened to be looking at, and a paused task only appeared
 * inside a composer dropdown. When a task blocked new work there was nowhere to
 * look and nothing to press.
 *
 * The rule this module encodes: only ever offer an action the backend will
 * accept. Cancelling requires the task to be paused with no live run, and a
 * running task can only be stopped — offering "discard" on a running task
 * produces a 409 and teaches the user that the buttons lie.
 */

/** Actions the user can take on a task, in the product's own vocabulary. */
export type AssistantTaskAction = 'continue' | 'discard' | 'stop'

export interface AssistantTaskView {
  id: string
  /** What the user asked for, taken from the task's first message. */
  label: string
  /** Why it is in this state, in plain language. */
  detail: string
  status: ProjectAssistantWorkItemStatus
  /** Product-language status: tasks, not durable-execution primitives. */
  statusLabel: string
  revision: number
  actions: AssistantTaskAction[]
}

/** Status wording. The store's vocabulary is not the user's. */
export function assistantTaskStatusLabel(status: ProjectAssistantWorkItemStatus): string {
  switch (status) {
    case 'active':
      return 'Running'
    case 'suspended':
      return 'Paused'
    case 'completed':
      return 'Done'
    case 'cancelled':
      return 'Discarded'
    default:
      return status
  }
}

/** Explains the state, preferring the reason the backend recorded. */
export function assistantTaskDetail(item: ProjectAssistantWorkItem): string {
  if (item.status === 'active') {
    return item.activeRunID
      ? 'Working now. Stop it to pause and take over.'
      : 'Finishing up — reload the project if it stays here.'
  }
  if (item.status === 'suspended') {
    switch (item.statusReason) {
      case 'provider restarted':
        return 'Interrupted when App Studio restarted. Its plan is intact.'
      case 'no_progress':
        return 'Stopped after repeating itself without making progress.'
      case 'failed':
        return 'The previous attempt failed.'
      case 'aborted':
        return 'You stopped this task. Continue where it left off.'
      default:
        return 'Paused before finishing. Its plan is intact.'
    }
  }
  if (item.status === 'cancelled') return 'Discarded. Its continuation authority was released.'
  return 'Finished.'
}

/**
 * The actions the backend will actually accept for a task.
 *
 * Mirrors cancelProjectAssistantWorkItem, which requires suspended AND no
 * active run, and the run-stop path, which is the only way to pause a running
 * task. An active task whose run is already gone offers nothing: reloading
 * releases it, and there is no button that does so.
 */
export function assistantTaskActions(item: ProjectAssistantWorkItem): AssistantTaskAction[] {
  if (item.status === 'active') return item.activeRunID ? ['stop'] : []
  if (item.status === 'suspended') return ['continue', 'discard']
  return []
}

export interface AssistantTaskLabelSource {
  id: string
  content: string
}

/** Derives the task's label from the message that started it. */
export function assistantTaskLabel(item: ProjectAssistantWorkItem, messages: AssistantTaskLabelSource[]): string {
  const root = messages.find((message) => message.id === item.rootMessageID)
  return root?.content.replace(/\s+/g, ' ').trim() || 'Untitled task'
}

/**
 * Builds the Activity task list: live and paused work first, because that is
 * what can be acted on, then finished work newest-first for context.
 */
export function assistantTaskViews(
  items: ProjectAssistantWorkItem[],
  messages: AssistantTaskLabelSource[],
): AssistantTaskView[] {
  const rank: Record<ProjectAssistantWorkItemStatus, number> = {
    active: 0,
    suspended: 1,
    completed: 2,
    cancelled: 3,
  }
  return [...items]
    .sort((a, b) => {
      const byStatus = (rank[a.status] ?? 9) - (rank[b.status] ?? 9)
      if (byStatus !== 0) return byStatus
      return (b.updatedAt ?? '').localeCompare(a.updatedAt ?? '')
    })
    .map((item) => ({
      id: item.id,
      label: assistantTaskLabel(item, messages),
      detail: assistantTaskDetail(item),
      status: item.status,
      statusLabel: assistantTaskStatusLabel(item.status),
      revision: item.revision,
      actions: assistantTaskActions(item),
    }))
}
