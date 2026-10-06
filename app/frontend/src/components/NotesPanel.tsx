import {useState} from 'react'
import {Check, Plus, Undo2} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {ScrollArea} from '@/components/ui/scroll-area'
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue} from '@/components/ui/select'
import {Textarea} from '@/components/ui/textarea'
import {Tooltip, TooltipContent, TooltipTrigger} from '@/components/ui/tooltip'
import {formatExact, formatTime} from '@/lib/format'
import type {Note, NoteType} from '@/lib/types'
import {cn} from '@/lib/utils'
import {NO_NOTES, useAppStore} from '@/store/app'

const NOTE_TYPES: NoteType[] = ['decision', 'blocker', 'heads_up', 'done', 'question']
const TYPE_STYLE: Record<NoteType, string> = {
  decision: 'text-note-decision',
  blocker: 'text-note-blocker',
  heads_up: 'text-note-heads-up',
  done: 'text-note-done',
  question: 'text-note-question',
}
const typeLabel = (t: NoteType) => t.replace('_', ' ') // dialog labels; the list shows the raw type

function AddNoteDialog({sessionId}: {sessionId: number}) {
  const addNote = useAppStore((s) => s.addNote)
  const [open, setOpen] = useState(false)
  const [type, setType] = useState<NoteType>('heads_up')
  const [content, setContent] = useState('')

  const submit = async () => {
    if (!content.trim()) return
    if (await addNote(sessionId, type, content.trim())) { // on failure a toast shows; keep the text
      setContent('')
      setOpen(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <DialogTrigger asChild>
            <Button variant="ghost" size="icon-sm" aria-label="Add note"><Plus/></Button>
          </DialogTrigger>
        </TooltipTrigger>
        <TooltipContent>Add note</TooltipContent>
      </Tooltip>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={(e) => { e.preventDefault(); void submit() }} className="flex flex-col gap-3">
          <DialogHeader>
            <DialogTitle>Add note</DialogTitle>
            <DialogDescription>Posted as you; agents read it.</DialogDescription>
          </DialogHeader>
          <Select value={type} onValueChange={(v) => setType(v as NoteType)}>
            <SelectTrigger aria-label="Note type" className="w-40"><SelectValue/></SelectTrigger>
            <SelectContent>
              {NOTE_TYPES.map((t) => <SelectItem key={t} value={t}>{typeLabel(t)}</SelectItem>)}
            </SelectContent>
          </Select>
          <Textarea
            autoFocus rows={4} value={content} onChange={(e) => setContent(e.target.value)} aria-label="Note content" placeholder="note"
            onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); void submit() } }}
          />
          <DialogFooter><Button type="submit" disabled={!content.trim()}>Add note</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function NoteRow({sessionId, note}: {sessionId: number; note: Note}) {
  // you / orchestrator / #id: agents are referenced by id, as on the cards.
  const author = useAppStore((s) => {
    if (note.author_agent_id === 0) return 'you'
    const a = s.data[sessionId]?.agents.find((x) => x.id === note.author_agent_id)
    return a?.role === 'orchestrator' ? 'orchestrator' : `#${note.author_agent_id}`
  })
  const setStatus = useAppStore((s) => s.setNoteStatus)
  const resolved = note.status === 'resolved'
  return (
    <li className={cn('group relative flex gap-2 text-[13px] leading-[18px]', resolved && 'opacity-55')} title={`${formatExact(note.created_at)}${resolved ? ' · resolved' : ''}`}>
      <p className={cn('min-w-0 flex-1 break-words whitespace-pre-wrap', resolved && 'line-through')}>
        <span className={cn('font-mono text-xs', TYPE_STYLE[note.type])}>{note.type}</span>{' '}
        <span className="font-mono text-xs text-muted-foreground">{author}</span>{' '}
        {note.content}
      </p>
      <time className="shrink-0 font-mono text-mini text-muted-foreground group-focus-within:opacity-0 group-hover:opacity-0" dateTime={note.created_at}>{formatTime(note.created_at)}</time>
      <Button
        variant="ghost" size="icon-xs" className="absolute top-0 right-0 size-[18px] bg-background opacity-0 group-focus-within:opacity-100 group-hover:opacity-100"
        aria-label={resolved ? 'Reopen note' : 'Resolve note'} title={resolved ? 'Reopen' : 'Resolve'} onClick={() => setStatus(sessionId, note.id, resolved ? 'open' : 'resolved')}
      >
        {resolved ? <Undo2/> : <Check/>}
      </Button>
    </li>
  )
}

export function NotesPanel({sessionId}: {sessionId: number | null}) {
  const notes = useAppStore((s) => (sessionId == null ? NO_NOTES : (s.data[sessionId]?.notes ?? NO_NOTES)))
  return (
    <section className="flex h-full min-h-0 flex-col" aria-label="Notes">
      <header className="flex h-9 shrink-0 items-center justify-between gap-2 pr-1.5 pl-3">
        <h2 className="text-title font-semibold">Notes</h2>
        {sessionId != null && <AddNoteDialog sessionId={sessionId}/>}
      </header>
      <ScrollArea className="min-h-0 flex-1">
        {notes.length === 0 ? (
          <p className="px-3 text-[13px] text-muted-foreground">no notes</p>
        ) : (
          <ul className="flex flex-col gap-1 px-3 pb-3">
            {[...notes].reverse().map((n) => <NoteRow key={n.id} sessionId={sessionId!} note={n}/>)}
          </ul>
        )}
      </ScrollArea>
    </section>
  )
}
