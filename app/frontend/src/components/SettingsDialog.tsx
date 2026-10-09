import {useState} from 'react'
import {Settings} from 'lucide-react'
import {Badge} from '@/components/ui/badge'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Input} from '@/components/ui/input'
import {Tip} from '@/components/ui/tooltip'
import {api} from '@/lib/api'
import {PROVIDER_NAMES, type ProviderInfo, type SessionProvider, type SubagentProvidersSettings} from '@/lib/types'
import {cn} from '@/lib/utils'

const ALL_PROVIDERS: SessionProvider[] = ['claude', 'codex', 'agy']

export function SettingsDialog({side = 'bottom'}: {side?: 'bottom' | 'right'}) {
  const [open, setOpen] = useState(false)
  const [compactAt, setCompactAt] = useState('') // '' = off
  const [providers, setProviders] = useState<ProviderInfo[]>([])
  const [subSettings, setSubSettings] = useState<SubagentProvidersSettings>({})
  const [loaded, setLoaded] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const onOpenChange = (o: boolean) => {
    setOpen(o)
    if (!o) return
    setLoaded(false)
    setError('')
    Promise.all([
      api.getSettings().catch(() => ({auto_compact_tokens: 0})),
      api.getProviders().catch(() => [
        {name: 'claude', available: true, default_model: ''},
        {name: 'codex', available: false, reason: 'Codex CLI not detected', default_model: ''},
        {name: 'agy', available: false, reason: 'Antigravity CLI not detected', default_model: ''},
      ] as ProviderInfo[]),
      api.getSubagentProviderSettings().catch(() => ({
        claude: {enabled: true},
      } as SubagentProvidersSettings)),
    ]).then(
      ([s, provs, sub]) => {
        setCompactAt(s.auto_compact_tokens ? String(s.auto_compact_tokens) : '')
        setProviders(provs)
        setSubSettings(sub ?? {})
        setLoaded(true)
      },
      (e) => setError(String(e)),
    )
  }
  const save = async () => {
    setBusy(true)
    setError('')
    try {
      await Promise.all([
        api.setSettings({auto_compact_tokens: compactAt.trim() === '' ? 0 : Number(compactAt)}),
        api.setSubagentProviderSettings(subSettings),
      ])
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
      <DialogContent className="sm:max-w-md max-h-[85vh] overflow-y-auto">
        <form onSubmit={(e) => { e.preventDefault(); void save() }} className="flex flex-col gap-3">
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

          <div className="flex flex-col gap-2 pt-2 border-t border-border-default">
            <div className="flex flex-col gap-0.5">
              <h3 className="text-[13px] font-semibold text-foreground">Sub-agent CLIs</h3>
              <span className="text-xs text-muted-foreground">Default status and models for sub-agent CLIs.</span>
            </div>
            <div className="flex flex-col gap-2">
              {ALL_PROVIDERS.map((pName) => {
                const info = providers.find((p) => p.name === pName) ?? {
                  name: pName,
                  available: pName === 'claude',
                  reason: pName === 'claude' ? undefined : `${PROVIDER_NAMES[pName]} not detected`,
                }
                const conf = subSettings[pName] ?? {
                  enabled: info.available,
                  default_model: '',
                }
                return (
                  <div key={pName} className="flex flex-col gap-2 rounded-lg border border-border-default p-2.5 bg-background">
                    <div className="flex items-center justify-between">
                      <span className="font-medium text-xs text-foreground">{PROVIDER_NAMES[pName] ?? pName}</span>
                      <Badge
                        variant="outline"
                        className={cn(
                          'text-[10px] h-4 px-1.5 font-mono',
                          info.available ? 'text-status-working border-status-working/30' : 'text-muted-foreground border-border-default'
                        )}
                      >
                        {info.available ? 'Detected' : 'Not detected'}
                      </Badge>
                    </div>

                    {!info.available && info.reason && (
                      <p className="text-[11px] text-muted-foreground leading-normal">
                        {info.reason}
                      </p>
                    )}

                    <label className="flex items-center gap-2 text-xs cursor-pointer select-none">
                      <input
                        type="checkbox"
                        checked={conf.enabled}
                        disabled={!loaded}
                        onChange={(e) =>
                          setSubSettings((prev) => ({
                            ...prev,
                            [pName]: { ...conf, enabled: e.target.checked },
                          }))
                        }
                        className="size-3.5 rounded border-border-default accent-primary cursor-pointer disabled:cursor-not-allowed"
                      />
                      <span>Enable by default</span>
                    </label>

                    <label className="flex flex-col gap-1 text-xs">
                      <span className="text-muted-foreground text-[11px]">Default model</span>
                      <Input
                        value={conf.default_model ?? ''}
                        disabled={!loaded}
                        placeholder={info.default_model || 'CLI default'}
                        onChange={(e) =>
                          setSubSettings((prev) => ({
                            ...prev,
                            [pName]: { ...conf, default_model: e.target.value },
                          }))
                        }
                        className="h-7 text-xs"
                      />
                    </label>
                  </div>
                )
              })}
            </div>
          </div>

          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
          <DialogFooter><Button type="submit" disabled={!loaded || busy}>{busy ? 'Saving...' : 'Save'}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
