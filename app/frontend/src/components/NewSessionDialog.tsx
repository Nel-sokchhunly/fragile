import {useEffect, useState, type ReactNode} from 'react'
import {FolderOpen} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Input} from '@/components/ui/input'
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue} from '@/components/ui/select'
import {Tip, Tooltip, TooltipContent, TooltipTrigger} from '@/components/ui/tooltip'
import {api} from '@/lib/api'
import {PROVIDER_NAMES, type ProviderInfo, type SessionProvider, type SubagentProvidersSettings} from '@/lib/types'
import {cn} from '@/lib/utils'
import {useAppStore} from '@/store/app'

const PROVIDER_HINTS: Partial<Record<SessionProvider, ReactNode>> = {
  codex: (
    <>Install Codex CLI 0.158+ and run <code>codex login</code> with ChatGPT. Your existing login is used; no API key needed. Text and image attachments supported.</>
  ),
  agy: (
    <>Requires Antigravity CLI (<code>agy</code>) installed and logged in. Your existing Google login is used; no API key needed.</>
  ),
}

const ALL_PROVIDERS: SessionProvider[] = ['claude', 'codex', 'agy']

// Wraps any trigger element (asChild) with the "new session" dialog.
export function NewSessionDialog({children, tip}: {children: ReactNode; tip?: string}) {
  const createSession = useAppStore((s) => s.createSession)
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [provider, setProvider] = useState<SessionProvider>('claude')
  const [availableProviders, setAvailableProviders] = useState<ProviderInfo[]>([])
  const [enabledSubProviders, setEnabledSubProviders] = useState<SessionProvider[]>(['claude'])
  const [dir, setDir] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) return
    let active = true
    Promise.all([
      api.getProviders().catch(() => [
        {name: 'claude', available: true, default_model: ''},
        {name: 'codex', available: false, reason: 'Codex CLI not detected', default_model: ''},
        {name: 'agy', available: false, reason: 'Antigravity CLI not detected', default_model: ''},
      ] as ProviderInfo[]),
      api.getSubagentProviderSettings().catch(() => ({
        claude: {enabled: true},
      } as SubagentProvidersSettings)),
    ]).then(([provs, settings]) => {
      if (!active) return
      setAvailableProviders(provs)

      // Pre-populate from global settings or defaults
      const selected: SessionProvider[] = []
      for (const p of provs) {
        if (!p.available) continue
        const conf = settings[p.name]
        if (conf !== undefined) {
          if (conf.enabled) selected.push(p.name)
        } else {
          selected.push(p.name)
        }
      }
      if (selected.length === 0) {
        const firstAvail = provs.find((p) => p.available)
        if (firstAvail) selected.push(firstAvail.name)
        else selected.push('claude')
      }
      setEnabledSubProviders(selected)
    })
    return () => { active = false }
  }, [open])

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
      await createSession(name.trim(), dir, provider, enabledSubProviders)
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
      <DialogContent className="sm:max-w-md max-h-[90vh] overflow-y-auto" onCloseAutoFocus={(e) => { e.preventDefault(); document.querySelector<HTMLElement>('[data-composer]')?.focus() }}>
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

          <div className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">Sub-agent CLIs</span>
            <div className="flex flex-col gap-1 rounded-md border border-border-default p-2 bg-background/50">
              {ALL_PROVIDERS.map((pName) => {
                const info = availableProviders.find((p) => p.name === pName) ?? {
                  name: pName,
                  available: pName === 'claude',
                  reason: pName === 'claude' ? undefined : `${PROVIDER_NAMES[pName]} CLI not detected`,
                }
                const checked = info.available && enabledSubProviders.includes(pName)
                const toggle = () => {
                  if (!info.available) return
                  setEnabledSubProviders((prev) =>
                    prev.includes(pName) ? prev.filter((p) => p !== pName) : [...prev, pName]
                  )
                }
                const row = (
                  <label
                    key={pName}
                    className={cn(
                      'flex items-center justify-between gap-2 py-0.5 text-xs select-none',
                      info.available ? 'cursor-pointer hover:text-foreground' : 'cursor-not-allowed opacity-50 text-muted-foreground'
                    )}
                  >
                    <span className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={checked}
                        disabled={!info.available}
                        onChange={toggle}
                        className="size-3.5 rounded border-border-default accent-primary cursor-pointer disabled:cursor-not-allowed"
                      />
                      <span>{PROVIDER_NAMES[pName] ?? pName}</span>
                    </span>
                    {!info.available && (
                      <span className="font-mono text-[11px] text-muted-foreground">unavailable</span>
                    )}
                  </label>
                )
                if (!info.available) {
                  return (
                    <Tip key={pName} content={info.reason || `${PROVIDER_NAMES[pName]} CLI is not available`}>
                      {row}
                    </Tip>
                  )
                }
                return row
              })}
            </div>
          </div>

          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} aria-label="Name" placeholder="name (default: directory name)"/>
          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
          <DialogFooter><Button type="submit" disabled={!ready}>{busy ? 'Starting...' : 'Create'}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
