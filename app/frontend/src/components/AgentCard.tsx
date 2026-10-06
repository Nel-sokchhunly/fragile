import {MessageSquare, Terminal} from 'lucide-react'
import {agentContext, agentState, formatExact} from '@/lib/format'
import {cn} from '@/lib/utils'
import type {Agent, Task} from '@/lib/types'

// Standalone: props in, no store access. The parent resolves task, latest line and elapsed time.
type Props = {
  agent: Agent
  task?: Task
  latestLine?: string
  elapsed: string
  selected?: boolean
  onSelect: () => void
}

const STATE_CLS = {running: 'text-status-working', exited: 'text-status-exited', stopped: 'text-status-exited', crashed: 'text-status-crashed'} as const

// Two lines: "#id name" + live state, then the latest activity (the task description when there is none yet;
// the full description and process details are in the hover title). Tool lines are "name {json}".
export function AgentCard({agent, task, latestLine, elapsed, selected, onSelect}: Props) {
  const title = task?.title ?? 'agent'
  const detail = latestLine || task?.description
  const ctx = agentContext(agent)
  const Icon = latestLine && /^\w+ \{/.test(latestLine) ? Terminal : MessageSquare
  const tip = [
    task?.description,
    `pid ${agent.pid ?? '-'} · started ${formatExact(agent.created_at)}${agent.exited_at ? ` · ended ${formatExact(agent.exited_at)}` : ''}`,
  ].filter(Boolean).join('\n')
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={!!selected}
      title={tip}
      className={cn(
        'flex w-full min-w-0 flex-col rounded-lg border border-border-default px-2.5 py-1.5 text-left transition-colors hover:border-border-strong hover:bg-accent focus-visible:border-border-strong focus-visible:outline-none',
        selected && 'border-border-strong bg-accent',
      )}
    >
      <span className="flex w-full justify-between gap-2 leading-5">
        <span className="min-w-0 truncate font-semibold"><span className="font-mono text-xs font-normal text-muted-foreground">#{agent.id}</span> {title}</span>
        <span className="flex shrink-0 gap-2 font-mono text-xs">
          {ctx && <span className={ctx.cls} title="context used / window">{ctx.text}</span>}
          <span className={STATE_CLS[agent.status]}>{agentState(agent)} {elapsed}</span>
        </span>
      </span>
      {detail && (
        <span className="flex min-w-0 items-center gap-1.5 text-[13px] leading-[18px] text-muted-foreground">
          {latestLine && <Icon className="size-3 shrink-0" aria-hidden/>}
          <span className="truncate">{detail}</span>
        </span>
      )}
    </button>
  )
}
