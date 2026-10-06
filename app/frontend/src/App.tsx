import {useDeferredValue, useEffect, useState} from 'react'
import {AgentsPanel} from '@/components/AgentsPanel'
import {NotesPanel} from '@/components/NotesPanel'
import {NewSessionDialog} from '@/components/NewSessionDialog'
import {SessionsSidebar} from '@/components/SessionsSidebar'
import {Button} from '@/components/ui/button'
import {AgentOutputView} from '@/components/chat/AgentOutputView'
import {ChatView} from '@/components/chat/ChatView'
import {ResizableHandle, ResizablePanel, ResizablePanelGroup} from '@/components/ui/resizable'
import {TooltipProvider} from '@/components/ui/tooltip'
import {useDefaultLayout, usePanelRef} from 'react-resizable-panels'
import {Plus} from 'lucide-react'
import {MOD} from '@/lib/keys'
import {useAppStore} from '@/store/app'

// Panel sizing in one place. The chat column never drops below CENTER_MIN: the right column shrinks
// toward RIGHT.min, and below SIDEBAR_COLLAPSE_BELOW the sidebar folds into its rail.
const SIDEBAR = {rail: '52px', min: '200px', default: '240px', max: '400px'}
const RIGHT = {min: '240px', default: '300px', max: '560px'}
const CENTER_MIN = '360px'
const SIDEBAR_COLLAPSE_BELOW = 900 // px of window width

// Below the collapse width, start with the sidebar already folded (the constraints can't all hold otherwise,
// and the panel group then refuses to collapse anything). Layout values are percentages of the window.
function narrowLayout(): Record<string, number> | undefined {
  const w = window.innerWidth
  if (w >= SIDEBAR_COLLAPSE_BELOW) return undefined
  const px = (v: string) => parseFloat(v)
  const sidebar = px(SIDEBAR.rail)
  const right = Math.max(px(RIGHT.min), Math.min(px(RIGHT.default), w - sidebar - px(CENTER_MIN)))
  const pct = (n: number) => (n / w) * 100
  return {sidebar: pct(sidebar), center: pct(w - sidebar - right), right: pct(right)}
}

// Non-blocking error messages from failed calls (the store removes them after a few seconds).
function Toasts() {
  const toasts = useAppStore((s) => s.toasts)
  return (
    <div className="pointer-events-none fixed right-4 bottom-4 z-[100] flex max-w-sm flex-col gap-2" aria-live="polite">
      {toasts.map((t) => <div key={t.id} role="alert" className="rounded-md border border-destructive bg-background px-3 py-1.5 text-[13px] text-destructive">{t.text}</div>)}
    </div>
  )
}

function EmptyCenter() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-[13px] text-muted-foreground">
      No sessions yet.
      <NewSessionDialog>
        <Button variant="outline" size="sm"><Plus/>New session <kbd className="font-mono text-muted-foreground">{MOD}N</kbd></Button>
      </NewSessionDialog>
    </div>
  )
}

export default function App() {
  const sessionId = useAppStore((s) => s.selectedSessionId)
  const agentId = useAppStore((s) => s.selectedAgentId)
  // Highlights (sidebar rows, agent cards) read the live ids and paint at once; the heavy center view follows.
  const viewSessionId = useDeferredValue(sessionId)
  const viewAgentId = useDeferredValue(agentId)
  const setCollapsed = useAppStore((s) => s.setSidebarCollapsed)
  const sidebar = usePanelRef()

  // Persist panel sizes (react-resizable-panels v4's replacement for autoSaveId).
  const main = useDefaultLayout({id: 'fragile-main-v2'})
  const [startNarrow] = useState(narrowLayout)
  const right = useDefaultLayout({id: 'fragile-right-v2'})

  // Animate only button/shortcut toggles (flex-grow transition); drags and window resizes stay instant.
  const [animating, setAnimating] = useState(false)
  const toggleSidebar = () => {
    const p = sidebar.current
    if (!p) return
    setAnimating(true)
    setTimeout(() => setAnimating(false), 200)
    p.isCollapsed() ? p.expand() : p.collapse()
  }
  // Fold the sidebar into its rail whenever the window is too narrow to keep the chat usable.
  useEffect(() => {
    const fit = () => {
      const p = sidebar.current
      if (!p) return false // panel group not mounted yet
      if (window.innerWidth < SIDEBAR_COLLAPSE_BELOW && !p.isCollapsed()) p.collapse()
      return true
    }
    const t = setInterval(() => fit() && clearInterval(t), 50) // initial fit, once the panel has registered
    window.addEventListener('resize', fit)
    return () => { clearInterval(t); window.removeEventListener('resize', fit) }
  }, [sidebar])
  // Keyboard: Cmd/Ctrl+B sidebar, +K or "/" composer, +N new session, +1..9 session, Esc agent view -> chat.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = (e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey
      const key = e.key.toLowerCase()
      const st = useAppStore.getState()
      const typing = e.target instanceof HTMLElement && e.target.closest('input, textarea, select, [contenteditable]')
      const dialogOpen = !!document.querySelector('[role=dialog]')
      if (mod && key === 'b') toggleSidebar()
      else if (mod && key === 'k') document.querySelector<HTMLElement>('[data-composer]')?.focus()
      else if (key === '/' && !e.metaKey && !e.ctrlKey && !typing && !dialogOpen) document.querySelector<HTMLElement>('[data-composer]')?.focus()
      else if (mod && key === 'n' && !dialogOpen) document.querySelector<HTMLElement>('[data-new-session]')?.click()
      else if (mod && /^[1-9]$/.test(key)) { const s = st.sessions[Number(key) - 1]; if (s) st.selectSession(s.id) }
      else if (key === 'escape' && st.selectedAgentId != null && !dialogOpen) st.selectAgent(null)
      else return
      e.preventDefault()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  return (
    <TooltipProvider delayDuration={300}>
      <ResizablePanelGroup id="fragile-main-v2" orientation="horizontal" className={animating ? 'panels-animating' : undefined} defaultLayout={startNarrow ?? main.defaultLayout} onLayoutChanged={main.onLayoutChanged}>
        <ResizablePanel
          id="sidebar" panelRef={sidebar} collapsible collapsedSize={SIDEBAR.rail} minSize={SIDEBAR.min} defaultSize={SIDEBAR.default} maxSize={SIDEBAR.max}
          onResize={() => setCollapsed(sidebar.current?.isCollapsed() ?? false)}
        >
          <SessionsSidebar onToggle={toggleSidebar}/>
        </ResizablePanel>
        <ResizableHandle/>
        <ResizablePanel id="center" minSize={CENTER_MIN}>
          {viewSessionId == null ? <EmptyCenter/>
            : viewAgentId != null ? <AgentOutputView key={viewAgentId} sessionId={viewSessionId} agentId={viewAgentId}/>
            : <ChatView key={viewSessionId} sessionId={viewSessionId}/>}
        </ResizablePanel>
        <ResizableHandle/>
        <ResizablePanel id="right" defaultSize={RIGHT.default} minSize={RIGHT.min} maxSize={RIGHT.max}>
          <ResizablePanelGroup id="fragile-right-v2" orientation="vertical" defaultLayout={right.defaultLayout} onLayoutChanged={right.onLayoutChanged}>
            <ResizablePanel id="agents" defaultSize="60%" minSize="20%"><AgentsPanel sessionId={sessionId}/></ResizablePanel>
            <ResizableHandle/>
            <ResizablePanel id="notes" defaultSize="40%" minSize="15%"><NotesPanel sessionId={sessionId}/></ResizablePanel>
          </ResizablePanelGroup>
        </ResizablePanel>
      </ResizablePanelGroup>
      <Toasts/>
    </TooltipProvider>
  )
}
