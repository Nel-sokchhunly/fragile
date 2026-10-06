import {useState, type ReactNode} from 'react'
import {FolderOpen} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Textarea} from '@/components/ui/textarea'
import {api} from '@/lib/api'
import {useAppStore} from '@/store/app'

// Wraps any trigger element (asChild) with the "new session" dialog.
export function NewSessionDialog({children}: {children: ReactNode}) {
  const createSession = useAppStore((s) => s.createSession)
  const [open, setOpen] = useState(false)
  const [task, setTask] = useState('')
  const [dir, setDir] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const pick = async () => {
    try {
      const d = await api.pickDirectory()
      if (d) setDir(d)
    } catch (e) {
      setError(String(e))
    }
  }
  const ready = !!task.trim() && !!dir && !busy
  const submit = async () => {
    if (!ready) return
    setBusy(true)
    setError('')
    try {
      await createSession(task.trim(), dir)
      setTask('')
      setOpen(false)
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{children}</DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={(e) => { e.preventDefault(); void submit() }} className="flex flex-col gap-3">
          <DialogHeader>
            <DialogTitle>New session</DialogTitle>
            <DialogDescription>Describe the task and pick the directory its agents work in. The orchestrator takes it from here.</DialogDescription>
          </DialogHeader>
          <Textarea
            autoFocus rows={4} value={task} onChange={(e) => setTask(e.target.value)} aria-label="Task" placeholder="e.g. Refactor the auth middleware"
            onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); void submit() } }}
          />
          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" size="sm" onClick={pick}><FolderOpen/> Choose directory</Button>
            <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={dir}>{dir || 'Required'}</span>
          </div>
          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
          <DialogFooter><Button type="submit" disabled={!ready}>{busy ? 'Starting...' : 'Create session'}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
