import {useNow} from '@/hooks/use-now'
import {formatElapsed} from '@/lib/format'
import {cn} from '@/lib/utils'
import {useAppStore} from '@/store/app'

// While working: shimmering label plus a live elapsed time. Own component so the 1s clock only ticks then.
function Elapsed({since}: {since: number}) {
  const now = useNow()
  return <span className="tabular-nums"> {formatElapsed(now - since)}</span>
}

export function StatusLabel({sessionId, working, label, className}: {sessionId: number; working: boolean; label: string; className?: string}) {
  const compacting = useAppStore((s) => s.compacting[sessionId] ?? 0)
  const busy = useAppStore((s) => s.busy[sessionId] ?? 0)
  const since = compacting || busy
  if (compacting) { working = true; label = 'compacting'; className = cn(className, 'text-status-decision') } // blue, apart from green working
  if (!working) return <span className={className}>{label}</span>
  return (
    <span className={className}>
      <span className={cn('working-shimmer', compacting ? '[--shimmer-base:var(--status-decision)]' : '[--shimmer-base:var(--status-working)]')}>{label}</span>
      {since > 0 && <Elapsed since={since}/>}
    </span>
  )
}
