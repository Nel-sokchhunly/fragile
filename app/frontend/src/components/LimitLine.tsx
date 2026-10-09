import {Tip} from '@/components/ui/tooltip'
import {useNow} from '@/hooks/use-now'
import {formatReset, limitPct, usageCls} from '@/lib/format'
import type {LimitWindow, SessionProvider} from '@/lib/types'
import {cn} from '@/lib/utils'
import {useAppStore} from '@/store/app'

const exact = (w: LimitWindow | null, now: number) =>
  w ? `resets in ${formatReset(w.resets_at, now)} (${new Date(w.resets_at * 1000).toLocaleString([], {hour12: false})})` : 'not reported yet'

const PROVIDER_LIMIT_UNAVAILABLE: Partial<Record<SessionProvider, {tip: string; label: string}>> = {
  codex: {
    tip: 'Codex subscription limits are not reported here',
    label: 'Codex · limits unavailable',
  },
  agy: {
    tip: 'Antigravity subscription limits are managed through Google Antigravity',
    label: 'Antigravity · limits unavailable',
  },
}

// Subscription limits as one mono line: "5h 8% · 7d 1%". Account-wide; the collapsed rail shows the 5h figure only.
// Reset times are in the hover title; its countdown ticks on a local 1-minute timer.
export function LimitLine({compact}: {compact?: boolean}) {
  const limit = useAppStore((s) => s.limit)
  const now = useNow(60_000)
  const provider = useAppStore((s) => s.sessions.find((x) => x.id === s.selectedSessionId)?.provider)
  const five = limit?.five_hour ?? null
  const seven = limit?.seven_day ?? null
  const title = `5h: ${limitPct(five)}, ${exact(five, now)}\n7d: ${limitPct(seven)}, ${exact(seven, now)}`
  const part = (label: string, w: LimitWindow | null) => (
    <span className={usageCls(w?.utilization ?? 0)}>{label} {limitPct(w)}</span>
  )
  if (provider && PROVIDER_LIMIT_UNAVAILABLE[provider]) {
    const unavail = PROVIDER_LIMIT_UNAVAILABLE[provider]!
    return (
      <Tip content={unavail.tip}>
        <span className="px-3 font-mono text-xs text-muted-foreground">{compact ? '—' : unavail.label}</span>
      </Tip>
    )
  }
  if (compact) return <Tip content={title} side="right"><span className={cn('font-mono text-mini', usageCls(five?.utilization ?? 0))}>{limitPct(five)}</span></Tip>
  return (
    <Tip content={title} side="top">
      <p className="truncate px-3 py-1.5 font-mono text-xs whitespace-nowrap text-muted-foreground">
        {part('5h', five)} · {part('7d', seven)}
      </p>
    </Tip>
  )
}
