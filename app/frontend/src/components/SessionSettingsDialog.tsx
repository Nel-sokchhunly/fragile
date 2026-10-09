import {useState} from 'react'
import {SlidersHorizontal} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Tip} from '@/components/ui/tooltip'
import {SessionConfigFields} from '@/components/SessionConfigFields'
import {api} from '@/lib/api'
import type {ProviderInfo, Session, SessionConfig} from '@/lib/types'
import {useAppStore} from '@/store/app'

// The session's own settings (sub-agent CLIs, auto-compact, orchestrator rules), editable mid-session.
export function SessionSettingsDialog({session}: {session: Session}) {
  const sessionUpdated = useAppStore((s) => s.sessionUpdated)
  const [open, setOpen] = useState(false)
  const [cfg, setCfg] = useState<SessionConfig | null>(null)
  const [providers, setProviders] = useState<ProviderInfo[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const onOpenChange = (o: boolean) => {
    setOpen(o)
    if (!o) return
    const {enabled_providers, auto_compact_tokens, orchestrator_rules} = session
    setCfg({enabled_providers, auto_compact_tokens, orchestrator_rules})
    setProviders(null)
    setError('')
    api.getProviders().then(setProviders, (e) => setError(String(e)))
  }
  const save = async () => {
    if (!cfg) return
    setBusy(true)
    setError('')
    try {
      sessionUpdated(await api.setSessionConfig(session.id, cfg))
      setOpen(false)
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Tip content="Session settings">
        <DialogTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label="Session settings"><SlidersHorizontal/></Button>
        </DialogTrigger>
      </Tip>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-md">
        <form onSubmit={(e) => { e.preventDefault(); void save() }} className="flex flex-col gap-2.5">
          <DialogHeader>
            <DialogTitle>Session settings</DialogTitle>
            <DialogDescription>This session only. Running sub-agents keep going; the orchestrator is told about CLI and rule changes.</DialogDescription>
          </DialogHeader>
          {cfg && <SessionConfigFields value={cfg} onChange={setCfg} providers={providers} disabled={busy}/>}
          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
          <DialogFooter><Button type="submit" disabled={!cfg || busy}>{busy ? 'Saving...' : 'Save'}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
