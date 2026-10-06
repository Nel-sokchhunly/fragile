import {PanelLeftClose, PanelLeftOpen, Plus} from 'lucide-react'
import {NewSessionDialog} from '@/components/NewSessionDialog'
import {Button} from '@/components/ui/button'
import {ScrollArea} from '@/components/ui/scroll-area'
import {Tooltip, TooltipContent, TooltipTrigger} from '@/components/ui/tooltip'
import {MOD} from '@/lib/keys'
import type {Session, SessionStatus} from '@/lib/types'
import {cn} from '@/lib/utils'
import {useAppStore} from '@/store/app'

// Status is plain coloured text (the rail keeps a dot so "needs you" stays visible when collapsed).
const STATUS: Record<SessionStatus, {label: string; text: string; dot: string}> = {
  working: {label: 'working', text: 'text-status-working', dot: 'bg-status-working'},
  done: {label: 'done', text: 'text-status-done', dot: 'bg-status-done/50'},
  needs_you: {label: 'needs you', text: 'text-status-needs-you', dot: 'bg-status-needs-you'},
}

const hint = (i: number) => (i < 9 ? ` (${MOD}${i + 1})` : '')

function SessionRow({session, index, selected}: {session: Session; index: number; selected: boolean}) {
  const select = useAppStore((s) => s.selectSession)
  const st = STATUS[session.status]
  return (
    <button
      type="button" onClick={() => select(session.id)} aria-current={selected ? 'true' : undefined}
      title={`#${session.id}${session.work_dir ? ` ${session.work_dir}` : ''}${hint(index)}`}
      className={cn(
        'flex w-full items-baseline gap-2 rounded-lg border border-transparent px-2 py-1 text-left hover:bg-sidebar-accent focus-visible:border-border-strong focus-visible:outline-none',
        selected && 'border-border-strong bg-background hover:bg-background',
      )}
    >
      <span className="min-w-0 flex-1 truncate">{session.title}</span>
      <span className={cn('shrink-0 font-mono text-xs', st.text)}>{st.label}</span>
    </button>
  )
}

function RailItem({session, index, selected}: {session: Session; index: number; selected: boolean}) {
  const select = useAppStore((s) => s.selectSession)
  const st = STATUS[session.status]
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button" onClick={() => select(session.id)} aria-label={`${session.title} (${st.label})`} aria-current={selected ? 'true' : undefined}
          className={cn(
            'relative flex size-8 items-center justify-center rounded-lg border border-transparent text-xs font-semibold text-text-secondary uppercase hover:bg-sidebar-accent focus-visible:border-border-strong focus-visible:outline-none',
            selected && 'border-border-strong bg-background hover:bg-background',
          )}
        >
          {session.title.charAt(0)}
          <span className={cn('absolute top-0.5 right-0.5 size-1.5 rounded-full', st.dot)} aria-hidden/>
        </button>
      </TooltipTrigger>
      <TooltipContent side="right">{session.title} · {st.label}{hint(index)}</TooltipContent>
    </Tooltip>
  )
}

export function SessionsSidebar({onToggle}: {onToggle: () => void}) {
  const sessions = useAppStore((s) => s.sessions)
  const selectedId = useAppStore((s) => s.selectedSessionId)
  const collapsed = useAppStore((s) => s.sidebarCollapsed)

  const toggle = (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon-sm" onClick={onToggle} aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}>
          {collapsed ? <PanelLeftOpen/> : <PanelLeftClose/>}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="right">{collapsed ? 'Expand' : 'Collapse'} sidebar ({MOD}B)</TooltipContent>
    </Tooltip>
  )
  const add = (
    <NewSessionDialog tip={`New session (${MOD}N)`}>
      <Button variant="ghost" size="icon-sm" aria-label="New session" data-new-session><Plus/></Button>
    </NewSessionDialog>
  )

  if (collapsed) {
    return (
      <nav aria-label="Sessions" className="flex h-full flex-col items-center gap-1 bg-sidebar py-1.5 text-sidebar-foreground">
        {toggle}
        {add}
        <ScrollArea className="min-h-0 w-full flex-1">
          <div className="flex flex-col items-center gap-1 py-1">
            {sessions.map((s, i) => <RailItem key={s.id} session={s} index={i} selected={s.id === selectedId}/>)}
          </div>
        </ScrollArea>
      </nav>
    )
  }
  return (
    <nav aria-label="Sessions" className="flex h-full flex-col bg-sidebar text-sidebar-foreground">
      <header className="flex h-10 shrink-0 items-center gap-0.5 pr-1.5 pl-3">
        <h2 className="flex-1 text-base font-semibold tracking-[-0.01em]">Fragile</h2>
        {toggle}
        {add}
      </header>
      <ScrollArea className="min-h-0 flex-1">
        <div className="flex flex-col gap-0.5 px-1.5 pb-2">
          {sessions.length === 0 && <p className="px-2 text-[13px] text-muted-foreground">No sessions yet.</p>}
          {sessions.map((s, i) => <SessionRow key={s.id} session={s} index={i} selected={s.id === selectedId}/>)}
        </div>
      </ScrollArea>
    </nav>
  )
}
