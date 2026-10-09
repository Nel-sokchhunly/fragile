import {useState} from 'react'
import {Settings} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Input} from '@/components/ui/input'
import {Tip} from '@/components/ui/tooltip'
import {api} from '@/lib/api'

export function SettingsDialog({side = 'bottom'}: {side?: 'bottom' | 'right'}) {
  const [open, setOpen] = useState(false)
  const [compactAt, setCompactAt] = useState('') // '' = off
  const [loaded, setLoaded] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const onOpenChange = (o: boolean) => {
    setOpen(o)
    if (!o) return
    setLoaded(false)
    setError('')
    api.getSettings().then(
      (s) => { setCompactAt(s.auto_compact_tokens ? String(s.auto_compact_tokens) : ''); setLoaded(true) },
      (e) => setError(String(e)),
    )
  }
  const save = async () => {
    setBusy(true)
    setError('')
    try {
      await api.setSettings({auto_compact_tokens: compactAt.trim() === '' ? 0 : Number(compactAt)})
      setOpen(false)
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Tip content="Settings" side={side}>
        <DialogTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label="Settings"><Settings/></Button>
        </DialogTrigger>
      </Tip>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={(e) => { e.preventDefault(); void save() }} className="flex flex-col gap-2.5">
          <DialogHeader>
            <DialogTitle>Settings</DialogTitle>
            <DialogDescription>Applies to all sessions.</DialogDescription>
          </DialogHeader>
          <label className="flex flex-col gap-1.5 text-[13px]">
            Auto-compact at (tokens)
            <Input
              type="number" min={0} step={1000} value={compactAt} onChange={(e) => setCompactAt(e.target.value)}
              disabled={!loaded} placeholder="off" autoFocus
            />
            <span className="text-xs text-muted-foreground">Compact the orchestrator after a turn once its context passes this. Off by default. Empty or 0 turns it off; otherwise 20000 to 1000000.</span>
          </label>
          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
          <DialogFooter><Button type="submit" disabled={!loaded || busy}>{busy ? 'Saving...' : 'Save'}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
