import {memo, useState} from 'react'
import {Virtuoso} from 'react-virtuoso'
import {CircleHelp, Square, Wrench} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Textarea} from '@/components/ui/textarea'
import {Composer} from '@/components/chat/Composer'
import {Markdown} from '@/components/Markdown'
import {formatTime} from '@/lib/format'
import type {ChatItem} from '@/lib/types'
import {cn} from '@/lib/utils'
import {NO_AGENTS, NO_CHAT, orchestratorRunning, useAppStore} from '@/store/app'

function EscalationBlock({item}: {item: Extract<ChatItem, {kind: 'escalation'}>}) {
  const answerEscalation = useAppStore((s) => s.answerEscalation)
  const [answer, setAnswer] = useState('')
  const e = item.escalation
  const open = e.status === 'open'
  const submit = () => {
    if (answer.trim()) void answerEscalation(e.id, answer.trim())
  }
  return (
    <div role="group" aria-label="Escalation" className={cn('rounded-lg border-2 p-3', open ? 'border-escalation bg-escalation/10' : 'border-border bg-muted/30')}>
      <div className={cn('mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide', open ? 'text-escalation-fg' : 'text-muted-foreground')}>
        <CircleHelp className="size-3.5" aria-hidden/> {open ? 'Needs your answer' : 'Answered'}
      </div>
      <p className="text-sm font-medium">{e.question}</p>
      {e.context && <p className="mt-1 text-xs text-muted-foreground">{e.context}</p>}
      {open ? (
        <div className="mt-2 flex items-end gap-2">
          <Textarea
            value={answer} onChange={(ev) => setAnswer(ev.target.value)} rows={1} aria-label="Your answer" placeholder="Answer the orchestrator..."
            className="max-h-32 min-h-9 resize-none bg-background"
            onKeyDown={(ev) => { if (ev.key === 'Enter' && !ev.shiftKey && !ev.nativeEvent.isComposing) { ev.preventDefault(); submit() } }}
          />
          <Button size="sm" onClick={submit} disabled={!answer.trim()}>Answer</Button>
        </div>
      ) : (
        <p className="mt-2 rounded bg-background px-2 py-1 text-sm">{e.answer}</p>
      )}
    </div>
  )
}

const Row = memo(function Row({item}: {item: ChatItem}) {
  // Virtuoso items can't use margins, so spacing is padding on the wrapper.
  return (
    <div className="px-4 py-1.5">
      {item.kind === 'user' && (
        <div className="flex flex-col items-end gap-0.5">
          <div className="max-w-[80%] rounded-2xl rounded-br-sm bg-primary px-3 py-2 text-sm whitespace-pre-wrap break-words text-primary-foreground">{item.text}</div>
          <time className="text-2xs text-muted-foreground" dateTime={item.at}>{formatTime(item.at)}</time>
        </div>
      )}
      {item.kind === 'assistant' && <div className="max-w-[90%]"><Markdown>{item.text}</Markdown></div>}
      {item.kind === 'tool' && (
        <div className="flex items-center gap-1.5 rounded border bg-muted/40 px-2 py-1 font-mono text-xs text-muted-foreground">
          <Wrench className="size-3 shrink-0" aria-hidden/>
          <span className="font-medium text-foreground">{item.name}</span>
          <span className="min-w-0 flex-1 truncate">{item.summary}</span>
        </div>
      )}
      {item.kind === 'escalation' && <EscalationBlock item={item}/>}
    </div>
  )
})

// Stopping kills the orchestrator and every sub-agent for good, so it asks first.
function StopButton({sessionId}: {sessionId: number}) {
  const stop = useAppStore((s) => s.stopSession)
  const [open, setOpen] = useState(false)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild><Button variant="ghost" size="sm"><Square/> Stop</Button></DialogTrigger>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Stop this session?</DialogTitle>
          <DialogDescription>This kills the orchestrator and all its sub-agents. A stopped session cannot be resumed.</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setOpen(false)}>Cancel</Button>
          <Button variant="destructive" onClick={() => { setOpen(false); void stop(sessionId) }}>Stop session</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function ChatView({sessionId}: {sessionId: number}) {
  const title = useAppStore((s) => s.sessions.find((x) => x.id === sessionId)?.title)
  const loaded = useAppStore((s) => !!s.data[sessionId])
  const chat = useAppStore((s) => s.data[sessionId]?.chat ?? NO_CHAT)
  const agents = useAppStore((s) => s.data[sessionId]?.agents ?? NO_AGENTS)
  const send = useAppStore((s) => s.sendMessage)
  const running = orchestratorRunning(agents)

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col">
      <header className="flex h-9 shrink-0 items-center gap-2 border-b px-4">
        <h1 className="min-w-0 flex-1 truncate text-sm font-semibold">{title}</h1>
        {running && <StopButton sessionId={sessionId}/>}
      </header>
      {/* Absolutely positioned list: its height never depends on percentage resolution inside flex. */}
      <div className="relative min-h-0 flex-1">
        {!loaded ? (
          <p className="p-6 text-sm text-muted-foreground">Loading...</p>
        ) : chat.length === 0 ? (
          <p className="p-6 text-sm text-muted-foreground">Nothing here yet.</p>
        ) : (
          <Virtuoso
            key={sessionId}
            data={chat}
            computeItemKey={(_, c) => c.id}
            initialTopMostItemIndex={{index: 'LAST', align: 'end'}}
            followOutput={(atBottom) => (atBottom ? 'smooth' : false)} // stop following once the user scrolls up
            itemContent={(_, item) => <Row item={item}/>}
            className="absolute inset-0"
          />
        )}
      </div>
      <Composer
        onSend={(t) => send(sessionId, t)} label="Message the orchestrator" placeholder="Message the orchestrator (Enter to send, Shift+Enter for newline)"
        disabledReason={loaded && !running ? "This session's orchestrator is not running (stopped, finished, or the app was restarted), so it can't take messages. The history stays viewable." : undefined}
      />
    </div>
  )
}
