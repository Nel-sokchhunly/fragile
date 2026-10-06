import type {Agent, Task} from '@/lib/types'

export function formatTime(iso: string) {
  return new Date(iso).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'})
}

// Compact clock: 12s, 2m05s, 1h05m.
export function formatElapsed(ms: number) {
  const s = Math.max(0, Math.floor(ms / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m${String(s % 60).padStart(2, '0')}s`
  return `${Math.floor(m / 60)}h${String(m % 60).padStart(2, '0')}m`
}

// Full local date and time, for hover titles next to the compact relative ones.
export const formatExact = (iso: string) => new Date(iso).toLocaleString([], {hour12: false})

// Running agents measure up to `now`, finished ones up to exited_at.
export function agentElapsed(a: Agent, now: number) {
  const end = a.exited_at ? Date.parse(a.exited_at) : now
  return formatElapsed(end - Date.parse(a.created_at))
}

export function agentName(a: Agent | undefined, t: Task | undefined) {
  if (!a) return 'Unknown agent'
  return a.role === 'orchestrator' ? 'Orchestrator' : (t?.title ?? `Agent #${a.id}`)
}

// "#2 storage" / "#1 orchestrator": the id is the handle shown everywhere an agent is referenced.
export function agentLabel(a: Agent | undefined, t: Task | undefined) {
  if (!a) return '#?'
  return `#${a.id} ${a.role === 'orchestrator' ? 'orchestrator' : (t?.title ?? 'agent')}`
}

// Exact process state: running, exited, exited(1), crashed(-1).
export function agentState(a: Agent) {
  if (a.status === 'running') return 'running'
  if (a.status === 'exited') return a.exit_code ? `exited(${a.exit_code})` : 'exited'
  return `crashed(${a.exit_code ?? '?'})`
}

// "2 running · 1 done" over the sub-agents (crashed ones counted separately), "no agents" when none.
export function agentSummary(agents: Agent[]) {
  const subs = agents.filter((a) => a.role === 'subagent')
  const n = (s: Agent['status']) => subs.filter((a) => a.status === s).length
  const parts = [`${n('running')} running`, `${n('exited')} done`]
  if (n('crashed')) parts.push(`${n('crashed')} crashed`)
  return subs.length ? parts.join(' · ') : 'no agents'
}
