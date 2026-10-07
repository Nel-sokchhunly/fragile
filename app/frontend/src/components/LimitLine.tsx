import {useNow} from '@/hooks/use-now'
import {formatReset, limitPct, usageCls} from '@/lib/format'
import type {LimitWindow} from '@/lib/types'
import {cn} from '@/lib/utils'
import {useAppStore} from '@/store/app'

const exact = (w: LimitWindow | null, now: number) =>
  w ? `resets in ${formatReset(w.resets_at, now)} (${new Date(w.resets_at * 1000).toLocaleString([], {hour12: false})})` : 'not reported yet'

// Subscription limits as one mono line: "5h 8% · 7d 1%". Account-wide; the collapsed rail shows the 5h figure only.
// Reset times are in the hover title; its countdown ticks on a local 1-minute timer.
export function LimitLine({compact}: {compact?: boolean}) {
  const limit = useAppStore((s) => s.limit)
  const now = useNow(60_000)
  const five = limit?.five_hour ?? null
  const seven = limit?.seven_day ?? null
  const title = `5h: ${limitPct(five)}, ${exact(five, now)}\n7d: ${limitPct(seven)}, ${exact(seven, now)}`
  const part = (label: string, w: LimitWindow | null) => (
    <span className={usageCls(w?.utilization ?? 0)}>{label} {limitPct(w)}</span>
  )
  if (compact) return <span title={title} className={cn('font-mono text-mini', usageCls(five?.utilization ?? 0))}>{limitPct(five)}</span>
  return (
    <p title={title} className="truncate px-3 py-1.5 font-mono text-xs whitespace-nowrap text-muted-foreground">
      {part('5h', five)} · {part('7d', seven)}
    </p>
  )
}
