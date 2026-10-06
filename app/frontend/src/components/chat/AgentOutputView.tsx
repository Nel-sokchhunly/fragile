import {memo, useEffect, useState} from 'react'
import {Virtuoso} from 'react-virtuoso'
import {ArrowLeft, ChevronRight, Wrench} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Collapsible, CollapsibleContent, CollapsibleTrigger} from '@/components/ui/collapsible'
import {Markdown} from '@/components/Markdown'
import {useNow} from '@/hooks/use-now'
import {agentElapsed, agentName} from '@/lib/format'
import type {Agent, AgentEvent} from '@/lib/types'
import {cn} from '@/lib/utils'
import {NO_EVENTS, useAppStore} from '@/store/app'

// View only (Phase 2 adds messaging). Payloads are JSON text; unknown shapes fall back to raw text.
function parse(payload: string): Record<string, unknown> {
  try { return JSON.parse(payload) } catch { return {text: payload} }
}

function Fold({icon, title, body, tone}: {icon?: boolean; title: string; body: string; tone?: string}) {
  const [open, setOpen] = useState(false)
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="rounded border bg-muted/40 font-mono text-xs">
      <CollapsibleTrigger className="flex w-full items-center gap-1.5 px-2 py-1 text-left text-muted-foreground hover:text-foreground">
        <ChevronRight className={cn('size-3 shrink-0 transition-transform', open && 'rotate-90')} aria-hidden/>
        {icon && <Wrench className="size-3 shrink-0" aria-hidden/>}
        <span className={cn('font-medium text-foreground', tone)}>{title}</span>
        {!open && <span className="min-w-0 flex-1 truncate">{body.replace(/\s+/g, ' ')}</span>}
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="max-h-72 overflow-auto border-t p-2 whitespace-pre-wrap break-words">{body}</pre>
      </CollapsibleContent>
    </Collapsible>
  )
}

const EventRow = memo(function EventRow({ev}: {ev: AgentEvent}) {
  const p = parse(ev.payload)
  return (
    <div className="px-4 py-1">
      {ev.event_type === 'assistant_text' && <Markdown>{String(p.text ?? '')}</Markdown>}
      {ev.event_type === 'tool_use' && <Fold icon title={String(p.name ?? 'tool')} body={JSON.stringify(p.input ?? {}, null, 2)}/>}
      {ev.event_type === 'tool_result' && <Fold title="result" tone={p.is_error ? 'text-destructive' : undefined} body={String(p.content ?? '')}/>}
      {!['assistant_text', 'tool_use', 'tool_result'].includes(ev.event_type) && <Fold title={ev.event_type} body={ev.payload}/>}
    </div>
  )
})

// Own component so the 1s clock tick re-renders only this label, not the virtualized list.
function Meta({agent, count}: {agent: Agent; count: number}) {
  const now = useNow()
  return <span className="shrink-0 text-xs text-muted-foreground">{agent.status} · {agentElapsed(agent, now)} · {count.toLocaleString()} events</span>
}

export function AgentOutputView({sessionId, agentId}: {sessionId: number; agentId: number}) {
  const agent = useAppStore((s) => s.data[sessionId]?.agents.find((a) => a.id === agentId))
  const task = useAppStore((s) => s.data[sessionId]?.tasks.find((t) => t.id === agent?.task_id))
  const events = useAppStore((s) => s.agentEvents[agentId] ?? NO_EVENTS)
  const loaded = useAppStore((s) => !!s.agentLoaded[agentId])
  const load = useAppStore((s) => s.loadAgentEvents)
  const back = useAppStore((s) => s.selectAgent)
  useEffect(() => { void load(agentId) }, [load, agentId]) // no-op once cached; live events keep appending

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col">
      <header className="flex h-9 shrink-0 items-center gap-2 border-b px-2">
        <Button variant="ghost" size="sm" onClick={() => back(null)} aria-label="Back to orchestrator chat"><ArrowLeft/> Chat</Button>
        <h1 className="min-w-0 flex-1 truncate text-sm font-semibold">{agentName(agent, task)}</h1>
        {agent && <Meta agent={agent} count={events.length}/>}
      </header>
      {/* Absolutely positioned list: its height never depends on percentage resolution inside flex. */}
      <div className="relative min-h-0 flex-1">
        {!loaded ? (
          <p className="p-6 text-sm text-muted-foreground">Loading...</p>
        ) : events.length === 0 ? (
          <p className="p-6 text-sm text-muted-foreground">No output from this agent yet.</p>
        ) : (
          <Virtuoso
            key={agentId}
            data={events}
            computeItemKey={(_, e) => e.id}
            initialTopMostItemIndex={{index: 'LAST', align: 'end'}}
            defaultItemHeight={28}
            followOutput={(atBottom) => (atBottom ? 'auto' : false)}
            increaseViewportBy={400}
            itemContent={(_, ev) => <EventRow ev={ev}/>}
            className="absolute inset-0"
          />
        )}
      </div>
      <p className="border-t px-4 py-2 text-xs text-muted-foreground">View only. Messaging sub-agents directly arrives in Phase 2.</p>
    </div>
  )
}
