import type {Agent, Note, Task} from '@/lib/types'

export type Column = 'working' | 'blocked' | 'done'
export const COLUMNS: {id: Column; label: string}[] = [
  {id: 'working', label: 'Working'},
  {id: 'blocked', label: 'Blocked'},
  {id: 'done', label: 'Done'},
]

// A Kanban card: a sub-agent task with its latest agent (a restart runs a new agent on the same task).
// open: the agent's unresolved blocker and question notes.
export type Card = {task: Task; agent: Agent; column: Column; open: number}

// Read-only board: cards place themselves from task status, process state and notes.
// done task -> Done; agent stopped, crashed or exited without finishing, or an open blocker note -> Blocked.
export function boardCards(tasks: Task[], agents: Agent[], notes: Note[]): Card[] {
  const latest = new Map<number, Agent>()
  for (const a of agents) {
    if (a.role !== 'subagent' || a.task_id == null) continue
    const prev = latest.get(a.task_id)
    if (!prev || a.id > prev.id) latest.set(a.task_id, a)
  }
  const cards: Card[] = []
  for (const task of tasks) {
    const agent = latest.get(task.id)
    if (!agent) continue // the orchestrator has no task; a task whose spawn failed has no agent to show
    const mine = notes.filter((n) => n.author_agent_id === agent.id && n.status === 'open')
    const blocker = mine.some((n) => n.type === 'blocker')
    const column: Column = task.status === 'done' ? 'done'
      : task.status === 'blocked' || agent.status !== 'running' || blocker ? 'blocked'
      : 'working'
    cards.push({task, agent, column, open: mine.filter((n) => n.type === 'blocker' || n.type === 'question').length})
  }
  return cards
}
