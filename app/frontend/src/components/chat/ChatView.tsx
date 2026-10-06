import {memo, useState} from 'react'
import {Virtuoso} from 'react-virtuoso'
import {CircleHelp, Wrench} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Textarea} from '@/components/ui/textarea'
import {Composer} from '@/components/chat/Composer'
import {Markdown} from '@/components/Markdown'
import {formatTime} from '@/lib/format'
import type {ChatItem} from '@/lib/types'
import {cn} from '@/lib/utils'
import {NO_CHAT, useAppStore} from '@/store/app'

function EscalationBlock({sessionId, item}: {sessionId: number; item: Extract<ChatItem, {kind: 'escalation'}>}) {
  const answerEscalation = useAppStore((s) => s.answerEscalation)
  const [answer, setAnswer] = useState('')
  const e = item.escalation
  const open = e.status === 'open'
  const submit = () => {
    if (answer.trim()) answerEscalation(sessionId, e.id, answer.trim())
  }
  return (
    <div role="group" aria-label="Escalation" className={cn('rounded-lg border-2 p-3', open ? 'border-amber-500 bg-amber-500/10' : 'border-border bg-muted/30')}>
      <div className={cn('mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide', open ? 'text-amber-600 dark:text-amber-400' : 'text-muted-foreground')}>
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

const Row = memo(function Row({sessionId, item}: {sessionId: number; item: ChatItem}) {
  // Virtuoso items can't use margins, so spacing is padding on the wrapper.
  return (
    <div className="px-4 py-1.5">
      {item.kind === 'user' && (
        <div className="flex flex-col items-end gap-0.5">
          <div className="max-w-[80%] rounded-2xl rounded-br-sm bg-primary px-3 py-2 text-sm whitespace-pre-wrap break-words text-primary-foreground">{item.text}</div>
          <time className="text-[10px] text-muted-foreground" dateTime={item.at}>{formatTime(item.at)}</time>
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
      {item.kind === 'escalation' && <EscalationBlock sessionId={sessionId} item={item}/>}
    </div>
  )
})

export function ChatView({sessionId}: {sessionId: number}) {
  const title = useAppStore((s) => s.sessions.find((x) => x.id === sessionId)?.title)
  const chat = useAppStore((s) => s.data[sessionId]?.chat ?? NO_CHAT)
  const send = useAppStore((s) => s.sendUserMessage)

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex h-9 shrink-0 items-center border-b px-4">
        <h1 className="truncate text-sm font-semibold">{title}</h1>
      </header>
      <div className="min-h-0 flex-1">
        {chat.length === 0 ? (
          <p className="p-6 text-sm text-muted-foreground">Give the orchestrator its task to get started.</p>
        ) : (
          <Virtuoso
            key={sessionId}
            data={chat}
            computeItemKey={(_, c) => c.id}
            initialTopMostItemIndex={chat.length - 1}
            followOutput={(atBottom) => (atBottom ? 'smooth' : false)} // stop following once the user scrolls up
            itemContent={(_, item) => <Row sessionId={sessionId} item={item}/>}
            className="h-full"
          />
        )}
      </div>
      <Composer onSend={(t) => send(sessionId, t)} label="Message the orchestrator" placeholder="Message the orchestrator (Enter to send, Shift+Enter for newline)"/>
    </div>
  )
}
