import {memo, useState} from 'react'
import {useNow} from '@/hooks/use-now'
import {Virtuoso} from 'react-virtuoso'
import {Square, Wrench} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Textarea} from '@/components/ui/textarea'
import {Tooltip, TooltipContent, TooltipTrigger} from '@/components/ui/tooltip'
import {Composer} from '@/components/chat/Composer'
import {Markdown} from '@/components/Markdown'
import {agentContext, agentLabel, agentState, agentSummary, formatElapsed, formatExact, formatTime} from '@/lib/format'
import type {ChatItem, SessionStatus} from '@/lib/types'
import {cn} from '@/lib/utils'
import {NO_AGENTS, NO_CHAT, orchestratorRunning, useAppStore} from '@/store/app'

// 'new' is not a backend status: a session with no agents yet (its orchestrator starts with the first message).
const STATUS_TEXT: Record<SessionStatus | 'new', {label: string; cls: string}> = {
  new: {label: 'new', cls: 'text-status-done'},
  working: {label: 'working', cls: 'text-status-working'},
  done: {label: 'done', cls: 'text-status-done'},
  needs_you: {label: 'needs you', cls: 'text-status-needs-you'},
}

// Option-button look from the design (amber text, amber border, fills on hover), used for the answer action.
const AMBER_BTN = 'rounded-md border border-escalation px-2.5 text-[13px] leading-[18px] text-escalation-fg transition-colors hover:bg-escalation-hover hover:text-foreground disabled:pointer-events-none disabled:opacity-40'

function EscalationBlock({sessionId, item}: {sessionId: number; item: Extract<ChatItem, {kind: 'escalation'}>}) {
  const answerEscalation = useAppStore((s) => s.answerEscalation)
  const [answer, setAnswer] = useState('')
  const e = item.escalation
  const open = e.status === 'open'
  // "#2 storage": the agent that asked.
  const from = useAppStore((s) => {
    const d = s.data[sessionId]
    const a = d?.agents.find((x) => x.id === e.agent_id)
    return agentLabel(a, d?.tasks.find((t) => t.id === a?.task_id))
  })
  const submit = () => {
    if (answer.trim()) void answerEscalation(e.id, answer.trim())
  }
  if (!open) {
    return (
      <div role="group" aria-label="Escalation" className="grid gap-0.5 rounded-lg border border-border-default px-3 py-1.5">
        <div className="flex gap-1.5 text-[13px] text-muted-foreground"><span>Escalation</span><span className="font-mono text-xs leading-[19px]">{from}</span><span>· answered</span></div>
        <div className="text-text-secondary">{e.question}</div>
        {e.context && <div className="text-[13px] text-muted-foreground">{e.context}</div>}
        <div><span className="text-status-decision">you</span> {e.answer}</div>
      </div>
    )
  }
  return (
    <div role="group" aria-label="Escalation" className="grid gap-0.5 rounded-lg border border-escalation bg-escalation-bg px-3 py-2 text-escalation-fg">
      <div className="flex gap-1.5"><span className="font-medium">Escalation</span><span className="font-mono text-xs leading-5 opacity-75">{from}</span></div>
      <div>{e.question}</div>
      {e.context && <div className="text-[13px] leading-[18px] opacity-75">{e.context}</div>}
      <div className="mt-1.5 flex items-end gap-1.5">
        <Textarea
          value={answer} onChange={(ev) => setAnswer(ev.target.value)} rows={1} aria-label="Your answer" placeholder="answer"
          className="max-h-32 min-h-8 resize-none border-escalation bg-transparent py-[5px] text-foreground placeholder:text-escalation-fg/60 focus-visible:border-escalation-fg"
          onKeyDown={(ev) => { if (ev.key === 'Enter' && !ev.shiftKey && !ev.nativeEvent.isComposing) { ev.preventDefault(); submit() } }}
        />
        <button type="button" className={cn(AMBER_BTN, 'h-8')} onClick={submit} disabled={!answer.trim()}>Answer</button>
      </div>
    </div>
  )
}

const Row = memo(function Row({sessionId, item}: {sessionId: number; item: ChatItem}) {
  // Virtuoso items can't use margins, so spacing is padding on the wrapper. The column is centred and capped.
  return (
    <div className="mx-auto max-w-[680px] px-6 py-[5px]">
      {item.kind === 'user' && (
        <div className="group flex items-end justify-end gap-2">
          <time className="font-mono text-mini leading-5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" dateTime={item.at} title={formatExact(item.at)}>{formatTime(item.at)}</time>
          <div className="max-w-[80%] rounded-lg bg-surface-sunken px-3 py-1.5 whitespace-pre-wrap break-words">{item.text}</div>
        </div>
      )}
      {item.kind === 'assistant' && <div className="font-serif text-assistant"><Markdown>{item.text}</Markdown></div>}
      {item.kind === 'tool' && (
        <div className="flex items-center gap-1.5 text-text-secondary" title={`${item.name} ${item.summary}`}>
          <Wrench className="size-3 shrink-0 text-muted-foreground" aria-hidden/>
          <span className="shrink-0 font-mono text-xs">{item.name}</span>
          <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground">{item.summary}</span>
        </div>
      )}
      {item.kind === 'escalation' && <EscalationBlock sessionId={sessionId} item={item}/>}
    </div>
  )
})

// Stopping kills the orchestrator and every sub-agent for good, so it asks first.
function StopButton({sessionId}: {sessionId: number}) {
  const stop = useAppStore((s) => s.stopSession)
  const [open, setOpen] = useState(false)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <DialogTrigger asChild><Button variant="ghost" size="icon-sm" aria-label="Stop session" className="shrink-0"><Square/></Button></DialogTrigger>
        </TooltipTrigger>
        <TooltipContent>Stop session</TooltipContent>
      </Tooltip>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Stop this session?</DialogTitle>
          <DialogDescription>Kills the orchestrator and all sub-agents. A stopped session cannot be resumed.</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setOpen(false)}>Cancel</Button>
          <Button variant="destructive" onClick={() => { setOpen(false); void stop(sessionId) }}>Stop session</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Module-level so Virtuoso doesn't remount them each render.
const Pad = () => <div className="h-3"/>
// Below the last row while the orchestrator is mid-turn, so a sent message never sits in silence.
// Its own component so the 1s clock only ticks while it is shown.
function Working({since}: {since: number}) {
  const now = useNow()
  return (
    <div role="status" className="mx-auto flex max-w-[680px] items-center gap-2.5 px-6 py-1.5 text-[13px]">
      <span className="working-dots flex items-center gap-1" aria-hidden><span/><span/><span/></span>
      <span className="working-shimmer font-medium">Working</span>
      <span className="font-mono text-xs text-muted-foreground tabular-nums">{formatElapsed(now - since)}</span>
    </div>
  )
}
const Footer = ({context}: {context?: {since: number}}) => (
  <div className="pb-3">{context?.since ? <Working since={context.since}/> : null}</div>
)

export function ChatView({sessionId}: {sessionId: number}) {
  const session = useAppStore((s) => s.sessions.find((x) => x.id === sessionId))
  const loaded = useAppStore((s) => !!s.data[sessionId])
  const chat = useAppStore((s) => s.data[sessionId]?.chat ?? NO_CHAT)
  const agents = useAppStore((s) => s.data[sessionId]?.agents ?? NO_AGENTS)
  const send = useAppStore((s) => s.sendMessage)
  const busySince = useAppStore((s) => s.busy[sessionId] ?? 0)
  const running = orchestratorRunning(agents)
  const working = running && busySince > 0 && session?.status !== 'needs_you' // an open escalation waits on the user, not the orchestrator
  const anyRunning = agents.some((a) => a.status === 'running') // sub-agents can outlive the orchestrator
  const lead = agents.findLast((a) => a.role === 'orchestrator') // latest: earlier ones failed to launch
  const ctx = agentContext(lead)
  const st = session && STATUS_TEXT[session.status === 'done' && !session.agent_count ? 'new' : session.status]

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col">
      <header className="flex h-[52px] shrink-0 items-center gap-2 border-b pr-1.5 pl-6">
        <div className="grid min-w-0 flex-1">
          <div className="flex min-w-0 items-baseline gap-2.5">
            <h1 className="min-w-0 truncate text-title font-semibold">{session?.title}</h1>
            {st && <span className={cn('shrink-0 font-mono text-xs', st.cls)}>{st.label}</span>}
          </div>
          <div className="flex min-w-0 items-baseline gap-2 font-mono text-xs leading-4 text-muted-foreground">
            <span className="min-w-0 truncate" title={lead ? `pid ${lead.pid ?? '-'} · started ${formatExact(lead.created_at)}` : undefined}>
              orchestrator{lead ? ` ${lead.status === 'running' ? `pid ${lead.pid ?? '-'}` : agentState(lead)}` : ' not started'} · {ctx && <><span className={ctx.cls}>ctx {ctx.text}</span> · </>}{agentSummary(agents)}
            </span>
          </div>
        </div>
        {anyRunning && <StopButton sessionId={sessionId}/>}
      </header>
      {/* Absolutely positioned list: its height never depends on percentage resolution inside flex. */}
      <div className="relative min-h-0 flex-1">
        {!loaded ? (
          <p className="p-6 text-[13px] text-muted-foreground">Loading...</p>
        ) : chat.length === 0 ? (
          <p className="p-6 text-[13px] text-muted-foreground">{lead ? 'No messages.' : 'Describe the task to start the orchestrator.'}</p>
        ) : (
          <Virtuoso
            key={sessionId}
            data={chat}
            computeItemKey={(_, c) => c.id}
            initialTopMostItemIndex={{index: 'LAST', align: 'end'}}
            followOutput={(atBottom) => (atBottom ? 'smooth' : false)} // stop following once the user scrolls up
            itemContent={(_, item) => <Row sessionId={sessionId} item={item}/>}
            context={{since: working ? busySince : 0}}
            components={{Header: Pad, Footer}}
            className="absolute inset-0"
          />
        )}
      </div>
      <Composer
        onSend={(t) => send(sessionId, t)} label="Message the orchestrator" placeholder="Message the orchestrator"
        disabledReason={loaded && lead && !running ? "Orchestrator not running (stopped, finished, or the app restarted): can't take messages. History stays viewable." : undefined}
      />
    </div>
  )
}
