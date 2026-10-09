import {useState, type ReactNode} from 'react'
import {FolderOpen} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Input} from '@/components/ui/input'
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue} from '@/components/ui/select'
import {Tip, Tooltip, TooltipContent, TooltipTrigger} from '@/components/ui/tooltip'
import {api} from '@/lib/api'
import type {ProviderInfo, SessionConfig, SessionProvider} from '@/lib/types'
import {SessionConfigFields, templateConfig} from '@/components/SessionConfigFields'
import {useAppStore} from '@/store/app'

const PROVIDER_HINTS: Partial<Record<SessionProvider, ReactNode>> = {
  codex: (
    <>Install Codex CLI 0.158+ and run <code>codex login</code> with ChatGPT. Your existing login is used; no API key needed. Text and image attachments supported.</>
  ),
  agy: (
    <>Requires Antigravity CLI (<code>agy</code>) installed and logged in. Your existing Google login is used; no API key needed.</>
  ),
}

// Wraps any trigger element (asChild) with the "new session" dialog.
export function NewSessionDialog({children, tip}: {children: ReactNode; tip?: string}) {
  const createSession = useAppStore((s) => s.createSession)
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [provider, setProvider] = useState<SessionProvider>('claude')
  const [dir, setDir] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [cfg, setCfg] = useState<SessionConfig | null>(null) // the Settings template, editable before creating
  const [providers, setProviders] = useState<ProviderInfo[] | null>(null)

  // Re-read the template and re-detect the CLIs each time it opens.
  const onOpenChange = (o: boolean) => {
    setOpen(o)
    if (!o) return
    setCfg(null)
    setProviders(null)
    api.getSettings().then((s) => setCfg(templateConfig(s)), (e) => setError(String(e)))
    api.getProviders().then(setProviders, (e) => setError(String(e)))
  }

  const pick = async () => {
    try {
      const d = await api.pickDirectory()
      if (d) setDir(d)
    } catch (e) {
      setError(String(e))
    }
  }
  const ready = !!dir && !!cfg && !busy
  const submit = async () => {
    if (!ready) return
    setBusy(true)
    setError('')
    try {
      await createSession(name.trim(), dir, provider, cfg!)
      setName('')
      setOpen(false)
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {tip ? (
        <Tooltip>
          <TooltipTrigger asChild><DialogTrigger asChild>{children}</DialogTrigger></TooltipTrigger>
          <TooltipContent side="right">{tip}</TooltipContent>
        </Tooltip>
      ) : <DialogTrigger asChild>{children}</DialogTrigger>}
      {/* After creating, focus goes to the composer rather than back to the (tooltipped) + button. */}
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-md" onCloseAutoFocus={(e) => { e.preventDefault(); document.querySelector<HTMLElement>('[data-composer]')?.focus() }}>
        <form onSubmit={(e) => { e.preventDefault(); void submit() }} className="flex flex-col gap-2.5">
          <DialogHeader>
            <DialogTitle>New session</DialogTitle>
            <DialogDescription>Working directory, and an optional name.</DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" size="sm" onClick={pick}><FolderOpen/> Directory</Button>
            <Tip content={dir}><span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground">{dir || 'working dir required'}</span></Tip>
          </div>
          <div className="flex items-center gap-2 text-xs">Provider
            <Select value={provider} onValueChange={(v) => setProvider(v as SessionProvider)}>
              <SelectTrigger aria-label="Provider" className="w-44"><SelectValue/></SelectTrigger>
              <SelectContent>
                <SelectItem value="claude">Claude Code</SelectItem>
                <SelectItem value="codex">Codex (ChatGPT)</SelectItem>
                <SelectItem value="agy">Antigravity (agy)</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {PROVIDER_HINTS[provider] && (
            <p className="text-xs text-muted-foreground">{PROVIDER_HINTS[provider]}</p>
          )}
          {cfg && <SessionConfigFields value={cfg} onChange={setCfg} providers={providers}/>}
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} aria-label="Name" placeholder="name (default: directory name)"/>
          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
          <DialogFooter><Button type="submit" disabled={!ready}>{busy ? 'Starting...' : 'Create'}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
