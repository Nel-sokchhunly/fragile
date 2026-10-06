import type {Agent, Task} from '@/lib/types'

export function formatTime(iso: string) {
  return new Date(iso).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'})
}

export function formatElapsed(ms: number) {
  const s = Math.max(0, Math.floor(ms / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, '0')}s`
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, '0')}m`
}

// Running agents measure up to `now`, finished ones up to exited_at.
export function agentElapsed(a: Agent, now: number) {
  const end = a.exited_at ? Date.parse(a.exited_at) : now
  return formatElapsed(end - Date.parse(a.created_at))
}

export function agentName(a: Agent | undefined, t: Task | undefined) {
  if (!a) return 'Unknown agent'
  return a.role === 'orchestrator' ? 'Orchestrator' : (t?.title ?? `Agent #${a.id}`)
}
