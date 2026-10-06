import {useEffect} from 'react'
import {Plus} from 'lucide-react'
import {AgentsPanel} from '@/components/AgentsPanel'
import {NewSessionDialog} from '@/components/NewSessionDialog'
import {NotesPanel} from '@/components/NotesPanel'
import {SessionsSidebar} from '@/components/SessionsSidebar'
import {AgentOutputView} from '@/components/chat/AgentOutputView'
import {ChatView} from '@/components/chat/ChatView'
import {Button} from '@/components/ui/button'
import {ResizableHandle, ResizablePanel, ResizablePanelGroup} from '@/components/ui/resizable'
import {TooltipProvider} from '@/components/ui/tooltip'
import {useDefaultLayout, usePanelRef} from 'react-resizable-panels'
import {useAppStore} from '@/store/app'

function EmptyCenter() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
      <h1 className="text-lg font-semibold">No sessions yet</h1>
      <p className="max-w-sm text-sm text-muted-foreground">A session is one task handed to an orchestrator, which spawns sub-agents to work on it. Start one to begin.</p>
      <NewSessionDialog><Button><Plus/> New session</Button></NewSessionDialog>
    </div>
  )
}

export default function App() {
  const sessionId = useAppStore((s) => s.selectedSessionId)
  const agentId = useAppStore((s) => s.selectedAgentId)
  const setCollapsed = useAppStore((s) => s.setSidebarCollapsed)
  const sidebar = usePanelRef()

  // Persist panel sizes (react-resizable-panels v4's replacement for autoSaveId).
  const main = useDefaultLayout({id: 'fragile-main'})
  const right = useDefaultLayout({id: 'fragile-right'})

  const toggleSidebar = () => {
    const p = sidebar.current
    if (p) p.isCollapsed() ? p.expand() : p.collapse()
  }
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'b') {
        e.preventDefault()
        toggleSidebar()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  return (
    <TooltipProvider delayDuration={300}>
      <ResizablePanelGroup id="fragile-main" orientation="horizontal" defaultLayout={main.defaultLayout} onLayoutChanged={main.onLayoutChanged}>
        <ResizablePanel
          id="sidebar" panelRef={sidebar} collapsible collapsedSize="52px" minSize="200px" defaultSize="240px" maxSize="400px"
          onResize={() => setCollapsed(sidebar.current?.isCollapsed() ?? false)}
        >
          <SessionsSidebar onToggle={toggleSidebar}/>
        </ResizablePanel>
        <ResizableHandle/>
        <ResizablePanel id="center" minSize="30%">
          {sessionId == null ? <EmptyCenter/>
            : agentId != null ? <AgentOutputView key={agentId} sessionId={sessionId} agentId={agentId}/>
            : <ChatView key={sessionId} sessionId={sessionId}/>}
        </ResizablePanel>
        <ResizableHandle/>
        <ResizablePanel id="right" defaultSize="300px" minSize="240px" maxSize="560px">
          <ResizablePanelGroup id="fragile-right" orientation="vertical" defaultLayout={right.defaultLayout} onLayoutChanged={right.onLayoutChanged}>
            <ResizablePanel id="agents" defaultSize="60%" minSize="20%"><AgentsPanel sessionId={sessionId}/></ResizablePanel>
            <ResizableHandle withHandle/>
            <ResizablePanel id="notes" defaultSize="40%" minSize="15%"><NotesPanel sessionId={sessionId}/></ResizablePanel>
          </ResizablePanelGroup>
        </ResizablePanel>
      </ResizablePanelGroup>
    </TooltipProvider>
  )
}
