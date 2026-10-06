import {PanelLeftClose, PanelLeftOpen, Plus} from 'lucide-react'
import {NewSessionDialog} from '@/components/NewSessionDialog'
import {Button} from '@/components/ui/button'
import {ScrollArea} from '@/components/ui/scroll-area'
import {Tooltip, TooltipContent, TooltipTrigger} from '@/components/ui/tooltip'
import type {Session, SessionStatus} from '@/lib/types'
import {cn} from '@/lib/utils'
import {useAppStore} from '@/store/app'

const STATUS: Record<SessionStatus, {label: string; dot: string; badge: string}> = {
  working: {label: 'working', dot: 'bg-status-working animate-pulse', badge: 'bg-status-working/15 text-status-working-fg'},
  done: {label: 'done', dot: 'bg-status-done/50', badge: 'bg-muted text-status-done'},
  needs_you: {label: 'needs you', dot: 'bg-status-needs-you ring-2 ring-status-needs-you/30', badge: 'bg-status-needs-you text-on-needs-you font-semibold'},
}

function SessionRow({session, selected}: {session: Session; selected: boolean}) {
  const select = useAppStore((s) => s.selectSession)
  const st = STATUS[session.status]
  return (
    <button
      type="button" onClick={() => select(session.id)} aria-current={selected ? 'true' : undefined}
      className={cn(
        'flex w-full items-center gap-2 rounded-md border-l-2 border-transparent px-2 py-1.5 text-left text-sm hover:bg-sidebar-accent focus-visible:ring-2 focus-visible:ring-sidebar-ring focus-visible:outline-none',
        selected && 'bg-sidebar-accent ring-1 ring-sidebar-ring', // clearly the open session, even if it also needs you
        session.status === 'needs_you' && 'border-status-needs-you',
        session.status === 'needs_you' && !selected && 'bg-status-needs-you/10',
      )}
    >
      <span className="min-w-0 flex-1 truncate">{session.title}</span>
      <span className={cn('shrink-0 rounded-full px-1.5 py-0.5 text-2xs leading-none', st.badge)}>{st.label}</span>
    </button>
  )
}

function RailItem({session, selected}: {session: Session; selected: boolean}) {
  const select = useAppStore((s) => s.selectSession)
  const st = STATUS[session.status]
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button" onClick={() => select(session.id)} aria-label={`${session.title} (${st.label})`} aria-current={selected ? 'true' : undefined}
          className={cn(
            'relative flex size-8 items-center justify-center rounded-md border text-xs font-semibold uppercase hover:bg-sidebar-accent focus-visible:ring-2 focus-visible:ring-sidebar-ring focus-visible:outline-none',
            selected && 'bg-sidebar-accent border-ring',
          )}
        >
          {session.title.charAt(0)}
          <span className={cn('absolute -top-1 -right-1 size-2.5 rounded-full', st.dot)} aria-hidden/>
        </button>
      </TooltipTrigger>
      <TooltipContent side="right">{session.title} · {st.label}</TooltipContent>
    </Tooltip>
  )
}

export function SessionsSidebar({onToggle}: {onToggle: () => void}) {
  const sessions = useAppStore((s) => s.sessions)
  const selectedId = useAppStore((s) => s.selectedSessionId)
  const collapsed = useAppStore((s) => s.sidebarCollapsed)
  const needsYou = sessions.filter((s) => s.status === 'needs_you').length

  const toggle = (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon-sm" onClick={onToggle} aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}>
          {collapsed ? <PanelLeftOpen/> : <PanelLeftClose/>}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="right">{collapsed ? 'Expand' : 'Collapse'} sidebar (Ctrl/Cmd+B)</TooltipContent>
    </Tooltip>
  )
  const add = (
    <NewSessionDialog>
      <Button variant="ghost" size="icon-sm" aria-label="New session"><Plus/></Button>
    </NewSessionDialog>
  )

  if (collapsed) {
    return (
      <nav aria-label="Sessions" className="flex h-full flex-col items-center gap-2 bg-sidebar py-2 text-sidebar-foreground">
        {toggle}
        {add}
        <ScrollArea className="min-h-0 w-full flex-1">
          <div className="flex flex-col items-center gap-2 py-1">
            {sessions.map((s) => <RailItem key={s.id} session={s} selected={s.id === selectedId}/>)}
          </div>
        </ScrollArea>
      </nav>
    )
  }
  return (
    <nav aria-label="Sessions" className="flex h-full flex-col bg-sidebar text-sidebar-foreground">
      <header className="flex h-9 shrink-0 items-center gap-1 border-b px-2">
        {toggle}
        <h2 className="flex-1 text-sm font-medium">Sessions</h2>
        {needsYou > 0 && <span className="rounded-full bg-status-needs-you px-1.5 text-2xs font-semibold text-on-needs-you">{needsYou}</span>}
        {add}
      </header>
      <ScrollArea className="min-h-0 flex-1">
        <div className="flex flex-col gap-0.5 p-2">
          {sessions.length === 0 && (
            <div className="flex flex-col items-start gap-2 p-2 text-xs text-muted-foreground">
              <p>No sessions yet.</p>
              <NewSessionDialog><Button size="sm" variant="outline"><Plus/> New session</Button></NewSessionDialog>
            </div>
          )}
          {sessions.map((s) => <SessionRow key={s.id} session={s} selected={s.id === selectedId}/>)}
        </div>
      </ScrollArea>
    </nav>
  )
}
