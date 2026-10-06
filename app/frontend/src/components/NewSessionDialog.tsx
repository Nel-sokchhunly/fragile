import {useState, type ReactNode} from 'react'
import {FolderOpen} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Input} from '@/components/ui/input'
import {Tooltip, TooltipContent, TooltipTrigger} from '@/components/ui/tooltip'
import {api} from '@/lib/api'
import {useAppStore} from '@/store/app'

// Wraps any trigger element (asChild) with the "new session" dialog.
export function NewSessionDialog({children, tip}: {children: ReactNode; tip?: string}) {
  const createSession = useAppStore((s) => s.createSession)
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
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
  const ready = !!dir && !busy
  const submit = async () => {
    if (!ready) return
    setBusy(true)
    setError('')
    try {
      await createSession(name.trim(), dir)
      setName('')
      setOpen(false)
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {tip ? (
        <Tooltip>
          <TooltipTrigger asChild><DialogTrigger asChild>{children}</DialogTrigger></TooltipTrigger>
          <TooltipContent side="right">{tip}</TooltipContent>
        </Tooltip>
      ) : <DialogTrigger asChild>{children}</DialogTrigger>}
      {/* After creating, focus goes to the composer rather than back to the (tooltipped) + button. */}
      <DialogContent className="sm:max-w-md" onCloseAutoFocus={(e) => { e.preventDefault(); document.querySelector<HTMLElement>('[data-composer]')?.focus() }}>
        <form onSubmit={(e) => { e.preventDefault(); void submit() }} className="flex flex-col gap-2.5">
          <DialogHeader>
            <DialogTitle>New session</DialogTitle>
            <DialogDescription>Working directory, and an optional name.</DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" size="sm" onClick={pick}><FolderOpen/> Directory</Button>
            <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground" title={dir}>{dir || 'working dir required'}</span>
          </div>
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} aria-label="Name" placeholder="name (default: directory name)"/>
          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
          <DialogFooter><Button type="submit" disabled={!ready}>{busy ? 'Starting...' : 'Create'}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
