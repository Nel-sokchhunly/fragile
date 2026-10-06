import {AlertTriangle, CheckCircle2, Loader2} from 'lucide-react'
import {Badge} from '@/components/ui/badge'
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

const STATUS = {
  running: {label: 'running', icon: Loader2, cls: 'text-status-working', spin: true},
  exited: {label: 'done', icon: CheckCircle2, cls: 'text-status-exited', spin: false},
  crashed: {label: 'crashed', icon: AlertTriangle, cls: 'text-status-crashed', spin: false},
} as const

export function AgentCard({agent, task, latestLine, elapsed, selected, onSelect}: Props) {
  const st = STATUS[agent.status]
  const Icon = st.icon
  const title = agent.role === 'orchestrator' ? 'Orchestrator' : (task?.title ?? `Agent #${agent.id}`)
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={!!selected}
      className={cn(
        'flex w-full flex-col gap-1 rounded-lg border bg-card p-2.5 text-left text-card-foreground transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
        selected && 'border-ring bg-accent ring-1 ring-ring',
      )}
    >
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{title}</span>
        {agent.role === 'orchestrator' && <Badge variant="outline" className="h-4 px-1 text-2xs">lead</Badge>}
      </div>
      <div className={cn('flex items-center gap-1.5 text-xs', st.cls)}>
        <Icon className={cn('size-3', st.spin && 'animate-spin')} aria-hidden/>
        <span>{st.label}</span>
        <span className="text-muted-foreground">· {elapsed}</span>
      </div>
      {task?.description && <p className="line-clamp-1 text-xs text-muted-foreground">{task.description}</p>}
      {latestLine && <p className="truncate rounded bg-muted/60 px-1.5 py-0.5 font-mono text-mini text-muted-foreground">{latestLine}</p>}
    </button>
  )
}
