import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from '@/components/ui/dialog'
import {useAppStore} from '@/store/app'

// Confirms deleting the session in store.confirmDelete (set by the sidebar's trash icon and Cmd/Ctrl+Backspace).
export function DeleteSessionDialog() {
  const id = useAppStore((s) => s.confirmDelete)
  const session = useAppStore((s) => s.sessions.find((x) => x.id === id))
  const setConfirm = useAppStore((s) => s.setConfirmDelete)
  const del = useAppStore((s) => s.deleteSession)
  return (
    <Dialog open={!!session} onOpenChange={(o) => { if (!o) setConfirm(null) }}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Delete session {session?.title}?</DialogTitle>
          <DialogDescription>Removes its chat, agents and notes from Fragile. Files in {session?.work_dir || 'its working directory'} are not touched.</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setConfirm(null)}>Cancel</Button>
          <Button variant="destructive" onClick={() => { setConfirm(null); if (session) void del(session.id) }}>Delete</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
