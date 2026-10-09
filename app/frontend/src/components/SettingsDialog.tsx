import {useState} from 'react'
import {Settings} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger} from '@/components/ui/dialog'
import {Input} from '@/components/ui/input'
import {Tip} from '@/components/ui/tooltip'
import {api} from '@/lib/api'
import {SessionConfigFields, templateConfig} from '@/components/SessionConfigFields'
import {PROVIDER_NAMES, PROVIDERS, type ProviderInfo, type SessionConfig, type Settings as Prefs} from '@/lib/types'

export function SettingsDialog({side = 'bottom'}: {side?: 'bottom' | 'right'}) {
  const [open, setOpen] = useState(false)
  const [prefs, setPrefs] = useState<Prefs | null>(null)
  const [providers, setProviders] = useState<ProviderInfo[] | null>(null)
  const [plugins, setPlugins] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // Re-detect the CLIs each time it opens.
  const onOpenChange = (o: boolean) => {
    setOpen(o)
    if (!o) return
    setPrefs(null)
    setProviders(null)
    setError('')
    api.getSettings().then(setPrefs, (e) => setError(String(e)))
    api.getProviders().then(setProviders, (e) => setError(String(e)))
    api.listUserPlugins().then(setPlugins, (e) => setError(String(e)))
  }
  const togglePlugin = (name: string, on: boolean) => setPrefs((p) => p && {
    ...p,
    subagent_disabled_plugins: [...(p.subagent_disabled_plugins ?? []).filter((x) => x !== name), ...(on ? [] : [name])],
  })
  // The template's enabled flags live in subagent_providers next to the default models.
  const setTemplate = (c: SessionConfig) => setPrefs((p) => p && {
    ...p,
    auto_compact_tokens: c.auto_compact_tokens,
    orchestrator_rules: c.orchestrator_rules,
    escalation_threshold: c.escalation_threshold,
    subagent_providers: Object.fromEntries(PROVIDERS.map((x) => [x, {...p.subagent_providers[x], enabled: c.enabled_providers.includes(x)}])),
  })
  const save = async () => {
    if (!prefs) return
    setBusy(true)
    setError('')
    try {
      await api.setSettings(prefs)
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
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-md">
        <form onSubmit={(e) => { e.preventDefault(); void save() }} className="flex flex-col gap-2.5">
          <DialogHeader>
            <DialogTitle>Settings</DialogTitle>
            <DialogDescription>Defaults for new sessions; change a session's own in its settings. Default models apply to every session.</DialogDescription>
          </DialogHeader>
          {prefs ? (
            <SessionConfigFields
              value={templateConfig(prefs)} onChange={setTemplate} providers={providers} disabled={busy}
              cliExtra={(p) => (
                <Input
                  aria-label={`${PROVIDER_NAMES[p]} default model`} placeholder="CLI default" className="h-7 w-40 font-mono text-xs"
                  value={prefs.subagent_providers[p]?.default_model ?? ''} disabled={busy}
                  onChange={(e) => setPrefs({...prefs, subagent_providers: {...prefs.subagent_providers, [p]: {enabled: false, ...prefs.subagent_providers[p], default_model: e.target.value.trim()}}})}
                />
              )}
            />
          ) : !error && <p className="text-[13px] text-muted-foreground">Loading...</p>}
          {prefs && plugins.length > 0 && (
            <fieldset className="flex flex-col gap-1.5 text-[13px]" disabled={busy}>
              <legend className="mb-1.5">Plugins for sub-agents</legend>
              {plugins.map((name) => (
                <label key={name} className="flex items-center gap-2">
                  <input
                    type="checkbox" className="size-3.5 accent-primary"
                    checked={!(prefs.subagent_disabled_plugins ?? []).includes(name)}
                    onChange={(e) => togglePlugin(name, e.target.checked)}
                  />
                  <span className="font-mono text-xs">{name}</span>
                </label>
              ))}
              <span className="text-xs text-muted-foreground">Unchecked plugins are not loaded by newly spawned sub-agents.</span>
            </fieldset>
          )}
          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
          <DialogFooter><Button type="submit" disabled={!prefs || busy}>{busy ? 'Saving...' : 'Save'}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
