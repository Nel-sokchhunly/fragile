import {useEffect, useState} from 'react'
import {ChevronRight} from 'lucide-react'
import {AgentCard} from '@/components/AgentCard'
import {Collapsible, CollapsibleContent, CollapsibleTrigger} from '@/components/ui/collapsible'
import {ScrollArea} from '@/components/ui/scroll-area'
import {useNow} from '@/hooks/use-now'
import {agentElapsed, agentSummary} from '@/lib/format'
import type {Agent, Task} from '@/lib/types'
import {cn} from '@/lib/utils'
import {latestLine, NO_AGENTS, NO_TASKS, useAppStore} from '@/store/app'

function Row({sessionId, agent, task, now}: {sessionId: number; agent: Agent; task?: Task; now: number}) {
  const line = useAppStore((s) => latestLine(s, sessionId, agent.id))
  const selected = useAppStore((s) => s.selectedAgentId === agent.id)
  const selectAgent = useAppStore((s) => s.selectAgent)
  return (
    <AgentCard
      agent={agent} task={task} latestLine={line} elapsed={agentElapsed(agent, now)}
      selected={selected} onSelect={() => selectAgent(agent.id)}
    />
  )
}

// Running and crashed agents need attention; exited and stopped ones are finished.
const isActive = (a: Agent) => a.status === 'running' || a.status === 'crashed'

// Newest finish first (ids break ties and cover a missing exited_at).
const byFinishDesc = (a: Agent, b: Agent) => (b.exited_at ?? '').localeCompare(a.exited_at ?? '') || b.id - a.id

export function AgentsPanel({sessionId}: {sessionId: number | null}) {
  const agents = useAppStore((s) => (sessionId == null ? NO_AGENTS : (s.data[sessionId]?.agents ?? NO_AGENTS)))
  const tasks = useAppStore((s) => (sessionId == null ? NO_TASKS : (s.data[sessionId]?.tasks ?? NO_TASKS)))
  const selectedId = useAppStore((s) => s.selectedAgentId)
  const now = useNow()
  const [open, setOpen] = useState(false) // the Finished group starts collapsed
  const subs = agents.filter((a) => a.role === 'subagent') // the orchestrator is the center chat, not a card
  const active = subs.filter(isActive)
  const finished = subs.filter((a) => !isActive(a)).sort(byFinishDesc)
  const selectedFinished = finished.some((a) => a.id === selectedId)
  useEffect(() => {
    if (selectedFinished) setOpen(true) // never hide the selected agent
  }, [selectedFinished, selectedId])
  const row = (a: Agent) => <Row key={a.id} sessionId={sessionId!} agent={a} now={now} task={tasks.find((t) => t.id === a.task_id)}/>

  return (
    <section className="flex h-full min-h-0 flex-col" aria-label="Agents">
      <header className="flex h-[52px] shrink-0 items-center justify-between gap-2 border-b px-3">
        <h2 className="text-title font-semibold">Agents</h2>
        <span className="truncate font-mono text-xs text-muted-foreground">{agentSummary(agents)}</span>
      </header>
      <ScrollArea className="min-h-0 flex-1">
        <div className="flex flex-col gap-1.5 p-2">
          {active.map(row)}
          {subs.length === 0 && <p className="px-1 text-[13px] text-muted-foreground">No sub-agents yet.</p>}
          {finished.length > 0 && (
            <Collapsible open={open} onOpenChange={setOpen} className="flex flex-col gap-1.5">
              <CollapsibleTrigger className={cn('flex w-full items-center gap-1.5 px-1 text-left font-mono text-xs text-muted-foreground hover:text-foreground', active.length > 0 && 'pt-1')}>
                <ChevronRight className={cn('size-3 shrink-0 transition-transform', open && 'rotate-90')} aria-hidden/>
                Finished ({finished.length})
              </CollapsibleTrigger>
              <CollapsibleContent className="flex flex-col gap-1.5">
                {finished.map(row)}
              </CollapsibleContent>
            </Collapsible>
          )}
        </div>
      </ScrollArea>
    </section>
  )
}
