import {Button} from '@/components/ui/button'
import {ResizableHandle, ResizablePanel, ResizablePanelGroup} from '@/components/ui/resizable'
import {useAppStore} from '@/store/app'

// Placeholder shell; the real layout comes with the sessions UI.
export default function App() {
  const tick = useAppStore((s) => s.tick)

  return (
    <ResizablePanelGroup orientation="horizontal" className="h-screen">
      <ResizablePanel defaultSize="25%" minSize="15%" className="p-4">
        <h2 className="text-sm font-medium text-muted-foreground">Sessions</h2>
      </ResizablePanel>
      <ResizableHandle withHandle/>
      <ResizablePanel defaultSize="75%" className="flex flex-col items-start gap-3 p-4">
        <h1 className="text-lg font-semibold">Fragile</h1>
        <p className="text-sm text-muted-foreground">
          {tick ? `tick #${tick.count} at ${tick.at}` : 'waiting for backend event...'}
        </p>
        <Button variant="outline" size="sm">Button</Button>
      </ResizablePanel>
    </ResizablePanelGroup>
  )
}
