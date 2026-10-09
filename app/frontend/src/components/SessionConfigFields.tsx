import type {ReactNode} from 'react'
import {Textarea} from '@/components/ui/textarea'
import {PROVIDER_NAMES, PROVIDERS, type ProviderInfo, type SessionConfig, type SessionProvider, type Settings} from '@/lib/types'

const RULES_PLACEHOLDER = `e.g.
agy flash: tests, docs, small UI tweaks
sonnet: features across a few files
opus: only architecture or hard bugs`

// Auto-compact slider stops, in tokens; 0 = off. The backend accepts 20k..1M.
const COMPACT_STOPS = [0, 50_000, 100_000, 150_000, 200_000, 300_000, 500_000, 1_000_000]
const nearestStop = (n: number) =>
  COMPACT_STOPS.reduce((best, s, i) => (Math.abs(s - n) < Math.abs(COMPACT_STOPS[best] - n) ? i : best), 0)
const tokens = (n: number) => (n >= 1_000_000 ? `${n / 1_000_000}M` : `${Math.round(n / 1000)}k`)

// The SessionConfig a new session gets from the Settings template (Go: Settings.sessionConfig).
export const templateConfig = (s: Settings): SessionConfig => ({
  enabled_providers: PROVIDERS.filter((p) => s.subagent_providers?.[p]?.enabled),
  auto_compact_tokens: s.auto_compact_tokens,
  orchestrator_rules: s.orchestrator_rules,
})

// The fields of a SessionConfig: the session dialogs edit one, Settings edits the template.
// providers: detection results (null while loading). cliExtra adds a control to a CLI's row.
export function SessionConfigFields({value, onChange, providers, disabled, cliExtra}: {
  value: SessionConfig
  onChange: (v: SessionConfig) => void
  providers: ProviderInfo[] | null
  disabled?: boolean
  cliExtra?: (p: SessionProvider) => ReactNode
}) {
  const toggle = (p: SessionProvider, on: boolean) =>
    onChange({...value, enabled_providers: PROVIDERS.filter((x) => (x === p ? on : value.enabled_providers.includes(x)))})
  return (
    <>
      <fieldset className="flex flex-col gap-1.5 text-[13px]" disabled={disabled}>
        <legend className="mb-1.5">Sub-agent CLIs</legend>
        {PROVIDERS.map((p) => {
          const info = providers?.find((x) => x.name === p)
          const on = value.enabled_providers.includes(p)
          return (
            <div key={p} className="flex flex-col gap-0.5">
              <div className="flex items-center gap-2">
                <label className="flex flex-1 items-center gap-2">
                  {/* An unavailable CLI can still be turned off, not on. */}
                  <input type="checkbox" checked={on} disabled={!on && info?.available === false} onChange={(e) => toggle(p, e.target.checked)} className="size-3.5 accent-primary"/>
                  {PROVIDER_NAMES[p]}
                  {info && !info.available && <span className="font-mono text-xs text-muted-foreground">unavailable</span>}
                </label>
                {cliExtra?.(p)}
              </div>
              {info && !info.available && info.reason && <span className="pl-5.5 text-xs text-muted-foreground">{info.reason}</span>}
            </div>
          )
        })}
        <span className="text-xs text-muted-foreground">CLIs the orchestrator may run sub-agents on; at least one. Each uses its own login and quota.</span>
      </fieldset>
      <label className="flex flex-col gap-1.5 text-[13px]">
        <span className="flex justify-between">Auto-compact<span className="font-mono text-xs">{value.auto_compact_tokens ? `at ${tokens(value.auto_compact_tokens)} tokens` : 'off'}</span></span>
        <input
          type="range" min={0} max={COMPACT_STOPS.length - 1} step={1} disabled={disabled} className="w-full accent-primary"
          value={nearestStop(value.auto_compact_tokens)} aria-valuetext={value.auto_compact_tokens ? tokens(value.auto_compact_tokens) : 'off'}
          onChange={(e) => onChange({...value, auto_compact_tokens: COMPACT_STOPS[Number(e.target.value)]})}
        />
        <span className="flex justify-between font-mono text-mini text-muted-foreground" aria-hidden>
          {COMPACT_STOPS.map((n) => <span key={n}>{n ? tokens(n) : 'off'}</span>)}
        </span>
        <span className="text-xs text-muted-foreground">Compact the orchestrator after a turn once its context passes this.</span>
      </label>
      <label className="flex flex-col gap-1.5 text-[13px]">
        Orchestrator rules
        <Textarea
          value={value.orchestrator_rules} disabled={disabled} placeholder={RULES_PLACEHOLDER} rows={4}
          className="max-h-48 font-mono text-xs" onChange={(e) => onChange({...value, orchestrator_rules: e.target.value})}
        />
        <span className="text-xs text-muted-foreground">Added to the orchestrator's prompt, e.g. which CLI and model to pick for which work.</span>
      </label>
    </>
  )
}
