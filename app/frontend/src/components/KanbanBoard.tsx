import {useMemo, useState} from 'react'
import {createPortal} from 'react-dom'
import {NotebookPen} from 'lucide-react'
import {AgentCard} from '@/components/AgentCard'
import {NotesPanel} from '@/components/NotesPanel'
import {Button} from '@/components/ui/button'
import {ScrollArea} from '@/components/ui/scroll-area'
import {Tip} from '@/components/ui/tooltip'
import {useNow} from '@/hooks/use-now'
import {boardCards, COLUMNS, type Card} from '@/lib/board'
import {agentElapsed, agentSummary} from '@/lib/format'
import {cn} from '@/lib/utils'
import {latestLine, NO_AGENTS, NO_NOTES, NO_TASKS, useAppStore} from '@/store/app'

function BoardCard({sessionId, card, now}: {sessionId: number; card: Card; now: number}) {
  const line = useAppStore((s) => latestLine(s, sessionId, card.agent.id))
  const selected = useAppStore((s) => s.selectedAgentId === card.agent.id)
  const selectAgent = useAppStore((s) => s.selectAgent)
  return (
    <AgentCard
      agent={card.agent} task={card.task} latestLine={line} elapsed={agentElapsed(card.agent, now)}
      selected={selected} open={card.open} onSelect={() => selectAgent(card.agent.id)}
    />
  )
}

// Read-only Kanban of the session's sub-agent tasks (the orchestrator is the center chat, not a card).
// Columns sit side by side when the panel is wide enough and stack when it is narrow.
// The notes board opens as an overlay drawer on the right.
export function KanbanBoard({sessionId}: {sessionId: number | null}) {
  const agents = useAppStore((s) => (sessionId == null ? NO_AGENTS : (s.data[sessionId]?.agents ?? NO_AGENTS)))
  const tasks = useAppStore((s) => (sessionId == null ? NO_TASKS : (s.data[sessionId]?.tasks ?? NO_TASKS)))
  const notes = useAppStore((s) => (sessionId == null ? NO_NOTES : (s.data[sessionId]?.notes ?? NO_NOTES)))
  const now = useNow()
  const [notesOpen, setNotesOpen] = useState(false)
  const cards = useMemo(() => boardCards(tasks, agents, notes), [tasks, agents, notes])
  const openNotes = notes.filter((n) => n.status === 'open').length

  return (
    <section className="flex h-full min-h-0 flex-col" aria-label="Board">
      <header className="flex h-[52px] shrink-0 items-center justify-between gap-2 border-b pr-1.5 pl-3">
        <h2 className="text-title font-semibold">Board</h2>
        <span className="flex min-w-0 items-center gap-1">
          <span className="truncate font-mono text-xs text-muted-foreground">{agentSummary(agents)}</span>
          <Tip content={notesOpen ? 'Hide notes' : 'Show notes'}>
            <Button variant="ghost" size="sm" className="h-7 gap-1 px-1.5 font-mono text-xs" aria-label={`Notes (${openNotes} open)`} aria-pressed={notesOpen} onClick={() => setNotesOpen(!notesOpen)}>
              <NotebookPen/>{openNotes > 0 && openNotes}
            </Button>
          </Tip>
        </span>
      </header>
      <ScrollArea className="min-h-0 flex-1">
        {cards.length === 0 ? <p className="p-3 text-[13px] text-muted-foreground">No sub-agents yet.</p> : (
          <div className="@container">
            <div className="grid gap-3 p-2 @xl:grid-cols-3">
              {COLUMNS.map((col) => {
                const list = cards.filter((c) => c.column === col.id)
                return (
                  <div key={col.id} className="flex min-w-0 flex-col gap-1.5" aria-label={col.label} role="group">
                    <h3 className={cn('px-1 font-mono text-xs text-muted-foreground', col.id === 'blocked' && list.length > 0 && 'text-status-crashed')}>
                      {col.label} ({list.length})
                    </h3>
                    {list.map((c) => <BoardCard key={c.task.id} sessionId={sessionId!} card={c} now={now}/>)}
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </ScrollArea>
      {notesOpen && createPortal(
        <aside className="fixed inset-y-0 right-0 z-50 w-[380px] max-w-full border-l bg-background shadow-lg">
          <NotesPanel sessionId={sessionId} onClose={() => setNotesOpen(false)}/>
        </aside>,
        document.body,
      )}
    </section>
  )
}
