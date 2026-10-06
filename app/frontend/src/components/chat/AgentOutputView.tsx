import {memo, useEffect, useState} from 'react'
import {Virtuoso} from 'react-virtuoso'
import {ArrowLeft, ChevronRight} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Collapsible, CollapsibleContent, CollapsibleTrigger} from '@/components/ui/collapsible'
import {Tooltip, TooltipContent, TooltipTrigger} from '@/components/ui/tooltip'
import {Markdown} from '@/components/Markdown'
import {useNow} from '@/hooks/use-now'
import {agentContext, agentElapsed, agentLabel, agentState, formatElapsed, formatExact} from '@/lib/format'
import type {Agent, AgentEvent} from '@/lib/types'
import {cn} from '@/lib/utils'
import {NO_EVENTS, useAppStore} from '@/store/app'

// View only. Payloads are JSON text; unknown shapes fall back to raw text.
function parse(payload: string): Record<string, unknown> {
  try { return JSON.parse(payload) } catch { return {text: payload} }
}

// One mono line (chevron, name, truncated body); opens to the full text.
function Fold({title, body, tone}: {title: string; body: string; tone?: string}) {
  const [open, setOpen] = useState(false)
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="font-mono text-xs leading-5 text-text-secondary">
      <CollapsibleTrigger className="flex w-full items-baseline gap-1.5 text-left hover:text-foreground">
        <ChevronRight className={cn('size-3 shrink-0 translate-y-[2px] text-muted-foreground transition-transform', open && 'rotate-90')} aria-hidden/>
        <span className={cn('shrink-0', tone)}>{title}</span>
        {!open && <span className="min-w-0 flex-1 truncate text-muted-foreground">{body.replace(/\s+/g, ' ')}</span>}
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="mt-0.5 max-h-72 overflow-auto rounded-md border border-border-default bg-surface-sunken p-2 whitespace-pre-wrap break-words">{body}</pre>
      </CollapsibleContent>
    </Collapsible>
  )
}

// Rows are a 40px mono relative-time column (exact time on hover) and the content.
const EventRow = memo(function EventRow({ev, start}: {ev: AgentEvent; start: number}) {
  const p = parse(ev.payload)
  return (
    <div className="mx-auto grid max-w-[680px] grid-cols-[40px_minmax(0,1fr)] gap-2 px-6 py-[3px]">
      <time className="font-mono text-mini leading-5 text-muted-foreground" dateTime={ev.created_at} title={formatExact(ev.created_at)}>{formatElapsed(Date.parse(ev.created_at) - start)}</time>
      <div className="min-w-0">
        {ev.event_type === 'assistant_text' && <div className="leading-5"><Markdown>{String(p.text ?? '')}</Markdown></div>}
        {ev.event_type === 'tool_use' && <Fold title={String(p.name ?? 'tool')} body={JSON.stringify(p.input ?? {}, null, 2)}/>}
        {ev.event_type === 'tool_result' && <Fold title="result" tone={p.is_error ? 'text-destructive' : undefined} body={String(p.content ?? '')}/>}
        {!['assistant_text', 'tool_use', 'tool_result'].includes(ev.event_type) && <Fold title={ev.event_type} body={ev.payload}/>}
      </div>
    </div>
  )
})

const STATE_CLS = {running: 'text-status-working', exited: 'text-status-exited', stopped: 'text-status-exited', crashed: 'text-status-crashed'} as const

// Own component so the 1s clock tick re-renders only this label, not the virtualized list.
function Meta({agent, count}: {agent: Agent; count: number}) {
  const now = useNow()
  const ctx = agentContext(agent)
  return (
    <div className="truncate font-mono text-xs leading-4 whitespace-nowrap text-muted-foreground" title={`pid ${agent.pid ?? '-'} · started ${formatExact(agent.created_at)}`}>
      {ctx && <><span className={ctx.cls} title="context used / window">ctx {ctx.text}</span> · </>}{agentElapsed(agent, now)} · {count.toLocaleString()} ev
    </div>
  )
}

const Pad = () => <div className="h-3"/>
// "streaming…" tail while the agent is alive (context carries that flag; the components stay module-level).
const Tail = ({context}: {context?: {live: boolean}}) => (
  <div className="mx-auto max-w-[680px] px-6 pt-[3px] pb-3">
    {context?.live && <div className="grid grid-cols-[40px_minmax(0,1fr)] gap-2 text-muted-foreground"><span/><span className="font-mono text-xs leading-5">streaming…</span></div>}
  </div>
)

export function AgentOutputView({sessionId, agentId}: {sessionId: number; agentId: number}) {
  const agent = useAppStore((s) => s.data[sessionId]?.agents.find((a) => a.id === agentId))
  const task = useAppStore((s) => s.data[sessionId]?.tasks.find((t) => t.id === agent?.task_id))
  const events = useAppStore((s) => s.agentEvents[agentId] ?? NO_EVENTS)
  const loaded = useAppStore((s) => !!s.agentLoaded[agentId])
  const load = useAppStore((s) => s.loadAgentEvents)
  const back = useAppStore((s) => s.selectAgent)
  useEffect(() => { void load(agentId) }, [load, agentId]) // no-op once cached; live events keep appending
  const start = agent ? Date.parse(agent.created_at) : 0

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col">
      <header className="flex h-[52px] shrink-0 items-center gap-2 border-b pr-6 pl-2">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-sm" onClick={() => back(null)} aria-label="Back to orchestrator chat"><ArrowLeft/></Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">Back to chat (Esc)</TooltipContent>
        </Tooltip>
        <div className="grid min-w-0 flex-1">
          <h1 className="truncate text-title font-semibold">
            {agentLabel(agent, task)}{agent && <> · <span className={cn('font-mono text-xs font-normal', STATE_CLS[agent.status])}>{agentState(agent)}</span></>}
          </h1>
          {agent && <Meta agent={agent} count={events.length}/>}
        </div>
      </header>
      {task?.description && <p className="truncate border-b px-6 py-1 text-[13px] text-text-secondary" title={task.description}>{task.description}</p>}
      {/* Absolutely positioned list: its height never depends on percentage resolution inside flex. */}
      <div className="relative min-h-0 flex-1">
        {!loaded ? (
          <p className="p-6 text-[13px] text-muted-foreground">Loading...</p>
        ) : events.length === 0 ? (
          <p className="p-6 text-[13px] text-muted-foreground">No output.</p>
        ) : (
          <Virtuoso
            key={agentId}
            data={events}
            computeItemKey={(_, e) => e.id}
            initialTopMostItemIndex={{index: 'LAST', align: 'end'}}
            defaultItemHeight={26}
            followOutput={(atBottom) => (atBottom ? 'auto' : false)}
            increaseViewportBy={400}
            itemContent={(_, ev) => <EventRow ev={ev} start={start}/>}
            components={{Header: Pad, Footer: Tail}}
            context={{live: agent?.status === 'running'}}
            className="absolute inset-0"
          />
        )}
      </div>
    </div>
  )
}
