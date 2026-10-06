import {useState, type ReactNode} from 'react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Input} from '@/components/ui/input'
import {useAppStore} from '@/store/app'

// Wraps any trigger element (asChild) with the "new session" dialog.
export function NewSessionDialog({children}: {children: ReactNode}) {
  const createSession = useAppStore((s) => s.createSession)
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')

  const submit = () => {
    if (!title.trim()) return
    createSession(title.trim())
    setTitle('')
    setOpen(false)
  }
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{children}</DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={(e) => { e.preventDefault(); submit() }} className="flex flex-col gap-3">
          <DialogHeader>
            <DialogTitle>New session</DialogTitle>
            <DialogDescription>Describe the task. The orchestrator takes it from here.</DialogDescription>
          </DialogHeader>
          <Input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} aria-label="Task title" placeholder="e.g. Refactor the auth middleware"/>
          <DialogFooter><Button type="submit" disabled={!title.trim()}>Create session</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
