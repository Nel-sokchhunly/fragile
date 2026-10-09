import {useCallback, useEffect, useState} from 'react'
import {Inbox} from 'lucide-react'
import {Markdown} from '@/components/Markdown'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Textarea} from '@/components/ui/textarea'
import {Tip} from '@/components/ui/tooltip'
import {api} from '@/lib/api'
import {on} from '@/lib/events'
import type {Escalation} from '@/lib/types'
import {useAppStore} from '@/store/app'

function Item({e, onJump}: {e: Escalation; onJump: () => void}) {
  const title = useAppStore((s) => s.sessions.find((x) => x.id === e.session_id)?.title ?? `#${e.session_id}`)
  const answerEscalation = useAppStore((s) => s.answerEscalation)
  const [answer, setAnswer] = useState('')
  const submit = () => {
    if (answer.trim()) void answerEscalation(e.id, answer.trim()) // same path as the chat; the 'escalation' event removes the row
  }
  return (
    <div role="group" aria-label="Escalation" className="grid gap-1 rounded-lg border border-escalation bg-escalation-bg px-3 py-2 text-escalation-fg">
      <button type="button" onClick={onJump} className="w-fit font-medium underline-offset-2 hover:underline">{title}</button>
      <div>{e.question}</div>
      {e.context && <div className="text-[13px] leading-[18px] opacity-75"><Markdown>{e.context}</Markdown></div>}
      <div className="mt-1.5 flex items-end gap-1.5">
        <Textarea
          value={answer} onChange={(ev) => setAnswer(ev.target.value)} rows={1} aria-label="Your answer" placeholder="answer"
          className="max-h-32 min-h-8 resize-none border-escalation bg-transparent py-[5px] text-foreground placeholder:text-escalation-fg/60 focus-visible:border-escalation-fg"
          onKeyDown={(ev) => { if (ev.key === 'Enter' && !ev.shiftKey && !ev.nativeEvent.isComposing) { ev.preventDefault(); submit() } }}
        />
        <button
          type="button" onClick={submit} disabled={!answer.trim()}
          className="h-8 rounded-md border border-escalation px-2.5 text-[13px] leading-[18px] text-escalation-fg transition-colors hover:bg-escalation-hover hover:text-foreground disabled:pointer-events-none disabled:opacity-40"
        >Answer</button>
      </div>
    </div>
  )
}

// Open escalations of all sessions in one dialog; the button carries the open count.
export function EscalationInbox({side = 'bottom'}: {side?: 'bottom' | 'right'}) {
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<Escalation[]>([])
  const select = useAppStore((s) => s.selectSession)

  const refresh = useCallback(() => { api.listOpenEscalations().then(setItems, () => {}) }, [])
  // 'escalation' fires when one is raised or answered; session_deleted drops that session's.
  useEffect(() => {
    refresh()
    const offs = [on('escalation', refresh), on('session_deleted', refresh)]
    return () => offs.forEach((off) => off())
  }, [refresh])

  return (
    <Dialog open={open} onOpenChange={(o) => { setOpen(o); if (o) refresh() }}>
      <Tip content="Escalation inbox" side={side}>
        <DialogTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label={`Escalation inbox (${items.length} open)`} className="relative">
            <Inbox/>
            {items.length > 0 && (
              <span className="absolute -top-0.5 -right-0.5 min-w-3.5 rounded-full bg-status-needs-you px-1 text-center font-mono text-[10px] leading-[14px] text-background">{items.length}</span>
            )}
          </Button>
        </DialogTrigger>
      </Tip>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Escalation inbox</DialogTitle>
          <DialogDescription>Open questions from every session. Answering here is the same as answering in the chat.</DialogDescription>
        </DialogHeader>
        {items.length === 0 && <p className="text-[13px] text-muted-foreground">Nothing waiting for you.</p>}
        <div className="flex flex-col gap-2">
          {items.map((e) => <Item key={e.id} e={e} onJump={() => { select(e.session_id); setOpen(false) }}/>)}
        </div>
      </DialogContent>
    </Dialog>
  )
}
