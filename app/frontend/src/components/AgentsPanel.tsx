import {Bot} from 'lucide-react'
import {AgentCard} from '@/components/AgentCard'
import {ScrollArea} from '@/components/ui/scroll-area'
import {useNow} from '@/hooks/use-now'
import {agentElapsed} from '@/lib/format'
import type {Agent, Task} from '@/lib/types'
import {latestLine, NO_AGENTS, NO_TASKS, useAppStore} from '@/store/app'

function Row({sessionId, agent, task, now}: {sessionId: number; agent: Agent; task?: Task; now: number}) {
  const line = useAppStore((s) => latestLine(s, sessionId, agent.id))
  const isLead = agent.role === 'orchestrator'
  const selected = useAppStore((s) => (isLead ? s.selectedAgentId === null : s.selectedAgentId === agent.id))
  const selectAgent = useAppStore((s) => s.selectAgent)
  return (
    <AgentCard
      agent={agent} task={task} latestLine={line} elapsed={agentElapsed(agent, now)}
      selected={selected} onSelect={() => selectAgent(isLead ? null : agent.id)}
    />
  )
}

export function AgentsPanel({sessionId}: {sessionId: number | null}) {
  const agents = useAppStore((s) => (sessionId == null ? NO_AGENTS : (s.data[sessionId]?.agents ?? NO_AGENTS)))
  const tasks = useAppStore((s) => (sessionId == null ? NO_TASKS : (s.data[sessionId]?.tasks ?? NO_TASKS)))
  const now = useNow()
  const subs = agents.filter((a) => a.role === 'subagent').length

  return (
    <section className="flex h-full min-h-0 flex-col" aria-label="Agents">
      <header className="flex h-9 shrink-0 items-center gap-2 border-b px-3">
        <Bot className="size-4 text-muted-foreground" aria-hidden/>
        <h2 className="text-sm font-medium">Agents</h2>
        <span className="text-xs text-muted-foreground">{subs}</span>
      </header>
      <ScrollArea className="min-h-0 flex-1">
        <div className="flex flex-col gap-2 p-2">
          {sessionId == null && <p className="p-2 text-xs text-muted-foreground">Select a session to see its agents.</p>}
          {agents.map((a) => <Row key={a.id} sessionId={sessionId!} agent={a} now={now} task={tasks.find((t) => t.id === a.task_id)}/>)}
          {sessionId != null && subs === 0 && (
            <p className="rounded-lg border border-dashed p-3 text-xs text-muted-foreground">
              No sub-agents yet. They appear here when the orchestrator spawns them.
            </p>
          )}
        </div>
      </ScrollArea>
    </section>
  )
}
