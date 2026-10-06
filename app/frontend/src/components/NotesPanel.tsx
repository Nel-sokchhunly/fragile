import {useState} from 'react'
import {Check, Plus, StickyNote, Undo2} from 'lucide-react'
import {Badge} from '@/components/ui/badge'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {ScrollArea} from '@/components/ui/scroll-area'
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue} from '@/components/ui/select'
import {Textarea} from '@/components/ui/textarea'
import {Tooltip, TooltipContent, TooltipTrigger} from '@/components/ui/tooltip'
import {agentName, formatTime} from '@/lib/format'
import type {Note, NoteType} from '@/lib/types'
import {cn} from '@/lib/utils'
import {NO_NOTES, useAppStore} from '@/store/app'

const NOTE_TYPES: NoteType[] = ['decision', 'blocker', 'heads_up', 'done', 'question']
const TYPE_STYLE: Record<NoteType, string> = {
  decision: 'bg-sky-500/15 text-sky-600 dark:text-sky-400',
  blocker: 'bg-red-500/15 text-red-600 dark:text-red-400',
  heads_up: 'bg-amber-500/15 text-amber-600 dark:text-amber-400',
  done: 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400',
  question: 'bg-violet-500/15 text-violet-600 dark:text-violet-400',
}
const typeLabel = (t: NoteType) => t.replace('_', ' ')

function AddNoteDialog({sessionId}: {sessionId: number}) {
  const addNote = useAppStore((s) => s.addNote)
  const [open, setOpen] = useState(false)
  const [type, setType] = useState<NoteType>('heads_up')
  const [content, setContent] = useState('')

  const submit = () => {
    if (!content.trim()) return
    addNote(sessionId, type, content.trim())
    setContent('')
    setOpen(false)
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
        <form onSubmit={(e) => { e.preventDefault(); submit() }} className="flex flex-col gap-3">
          <DialogHeader>
            <DialogTitle>Add note</DialogTitle>
            <DialogDescription>Posted to the session board as you; agents can read it.</DialogDescription>
          </DialogHeader>
          <Select value={type} onValueChange={(v) => setType(v as NoteType)}>
            <SelectTrigger aria-label="Note type" className="w-40"><SelectValue/></SelectTrigger>
            <SelectContent>
              {NOTE_TYPES.map((t) => <SelectItem key={t} value={t}>{typeLabel(t)}</SelectItem>)}
            </SelectContent>
          </Select>
          <Textarea
            autoFocus rows={4} value={content} onChange={(e) => setContent(e.target.value)} aria-label="Note content" placeholder="What should the team know?"
            onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); submit() } }}
          />
          <DialogFooter><Button type="submit" disabled={!content.trim()}>Add note</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function NoteRow({sessionId, note}: {sessionId: number; note: Note}) {
  const author = useAppStore((s) => {
    if (note.author_agent_id === 0) return 'You'
    const d = s.data[sessionId]
    const a = d?.agents.find((x) => x.id === note.author_agent_id)
    return agentName(a, d?.tasks.find((t) => t.id === a?.task_id))
  })
  const toggle = useAppStore((s) => s.toggleNoteResolved)
  const resolved = note.status === 'resolved'
  return (
    <li className={cn('group flex flex-col gap-1 rounded-lg border bg-card p-2 text-sm', resolved && 'opacity-55')}>
      <div className="flex items-center gap-1.5 text-xs">
        <Badge variant="secondary" className={cn('h-4 border-0 px-1.5 text-[10px]', TYPE_STYLE[note.type])}>{typeLabel(note.type)}</Badge>
        <span className="min-w-0 flex-1 truncate font-medium">{author}</span>
        {resolved && <span className="text-muted-foreground">resolved</span>}
        <time className="text-muted-foreground" dateTime={note.created_at}>{formatTime(note.created_at)}</time>
        <Button
          variant="ghost" size="icon-xs" className="opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
          aria-label={resolved ? 'Reopen note' : 'Resolve note'} onClick={() => toggle(sessionId, note.id)}
        >
          {resolved ? <Undo2/> : <Check/>}
        </Button>
      </div>
      <p className={cn('whitespace-pre-wrap break-words', resolved && 'line-through')}>{note.content}</p>
    </li>
  )
}

export function NotesPanel({sessionId}: {sessionId: number | null}) {
  const notes = useAppStore((s) => (sessionId == null ? NO_NOTES : (s.data[sessionId]?.notes ?? NO_NOTES)))
  return (
    <section className="flex h-full min-h-0 flex-col" aria-label="Notes">
      <header className="flex h-9 shrink-0 items-center gap-2 border-b px-3">
        <StickyNote className="size-4 text-muted-foreground" aria-hidden/>
        <h2 className="flex-1 text-sm font-medium">Notes</h2>
        {sessionId != null && <AddNoteDialog sessionId={sessionId}/>}
      </header>
      <ScrollArea className="min-h-0 flex-1">
        {notes.length === 0 ? (
          <p className="p-4 text-xs text-muted-foreground">
            {sessionId == null ? 'Select a session to see its notes.' : 'No notes yet. Agents post decisions, blockers and heads-ups here, and you can add your own.'}
          </p>
        ) : (
          <ul className="flex flex-col gap-2 p-2">
            {[...notes].reverse().map((n) => <NoteRow key={n.id} sessionId={sessionId!} note={n}/>)}
          </ul>
        )}
      </ScrollArea>
    </section>
  )
}
