import {AgentCard} from '@/components/AgentCard'
import {ScrollArea} from '@/components/ui/scroll-area'
import {useNow} from '@/hooks/use-now'
import {agentElapsed, agentSummary} from '@/lib/format'
import type {Agent, Task} from '@/lib/types'
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

export function AgentsPanel({sessionId}: {sessionId: number | null}) {
  const agents = useAppStore((s) => (sessionId == null ? NO_AGENTS : (s.data[sessionId]?.agents ?? NO_AGENTS)))
  const tasks = useAppStore((s) => (sessionId == null ? NO_TASKS : (s.data[sessionId]?.tasks ?? NO_TASKS)))
  const now = useNow()
  const subs = agents.filter((a) => a.role === 'subagent') // the orchestrator is the center chat, not a card

  return (
    <section className="flex h-full min-h-0 flex-col" aria-label="Agents">
      <header className="flex h-[52px] shrink-0 items-center justify-between gap-2 border-b px-3">
        <h2 className="text-title font-semibold">Agents</h2>
        <span className="truncate font-mono text-xs text-muted-foreground">{agentSummary(agents)}</span>
      </header>
      <ScrollArea className="min-h-0 flex-1">
        <div className="flex flex-col gap-1.5 p-2">
          {subs.map((a) => <Row key={a.id} sessionId={sessionId!} agent={a} now={now} task={tasks.find((t) => t.id === a.task_id)}/>)}
          {subs.length === 0 && <p className="px-1 text-[13px] text-muted-foreground">No sub-agents yet.</p>}
        </div>
      </ScrollArea>
    </section>
  )
}
