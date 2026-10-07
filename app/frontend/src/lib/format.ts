import type {Agent, LimitWindow, Task} from '@/lib/types'

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

// Exact process state: running, exited, exited(1), stopped, crashed(-1).
export function agentState(a: Agent) {
  if (a.status === 'running') return 'running'
  if (a.status === 'exited') return a.exit_code ? `exited(${a.exit_code})` : 'exited'
  if (a.status === 'stopped') return 'stopped'
  return `crashed(${a.exit_code ?? '?'})`
}

// "2 running · 1 done" over the sub-agents (stopped and crashed ones counted separately), "no agents" when none.
export function agentSummary(agents: Agent[]) {
  const subs = agents.filter((a) => a.role === 'subagent')
  const n = (s: Agent['status']) => subs.filter((a) => a.status === s).length
  const parts = [`${n('running')} running`, `${n('exited')} done`]
  if (n('stopped')) parts.push(`${n('stopped')} stopped`)
  if (n('crashed')) parts.push(`${n('crashed')} crashed`)
  return subs.length ? parts.join(' · ') : 'no agents'
}

// 28726 -> "28.7k", 1000000 -> "1M".
function tokens(n: number) {
  const f = (v: number, u: string) => `${v >= 100 || Number.isInteger(v) ? Math.round(v) : v.toFixed(1)}${u}`
  return n >= 1e6 ? f(n / 1e6, 'M') : n >= 1e3 ? f(n / 1e3, 'k') : String(n)
}

// Amber from 80%, red from 95% (fraction 0..1); muted below.
export const usageCls = (frac: number) => (frac >= 0.95 ? 'text-status-crashed' : frac >= 0.8 ? 'text-escalation-fg' : 'text-muted-foreground')

// "28.7k/1M 3%" for an agent's context, with its colour class; undefined while unknown.
export function agentContext(a: Agent | undefined) {
  if (!a?.context_window || !a.context_used) return undefined
  const frac = a.context_used / a.context_window
  return {text: `${tokens(a.context_used)}/${tokens(a.context_window)} ${Math.round(frac * 100)}%`, cls: usageCls(frac)}
}

// "claude-opus-5-5" -> "Opus 5.5", "claude-haiku-4-5-20251001" -> "Haiku 4.5", "claude-opus-5-5[1m]" -> "Opus 5.5 1M";
// anything else is shown as is.
export function modelName(id: string) {
  const long = id.endsWith('[1m]')
  const m = /^claude-([a-z]+)-(\d+)(?:-(\d{1,2}))?(?:-\d{8})?$/.exec(long ? id.slice(0, -4) : id)
  if (!m) return id
  return `${m[1][0].toUpperCase()}${m[1].slice(1)} ${m[2]}${m[3] ? `.${m[3]}` : ''}${long ? ' 1M' : ''}`
}

// "2h13m", "5d", "45m": time until a unix-seconds reset.
export function formatReset(resetsAt: number, now: number) {
  const m = Math.max(0, Math.floor((resetsAt * 1000 - now) / 60000))
  return m >= 1440 ? `${Math.floor(m / 1440)}d` : m >= 60 ? `${Math.floor(m / 60)}h${String(m % 60).padStart(2, '0')}m` : `${m}m`
}

export const limitPct = (w: LimitWindow | null | undefined) => (w ? `${Math.round(w.utilization * 100)}%` : '\u2014')

// "812 B", "14 KB", "2.3 MB" for a file size in bytes.
export function formatBytes(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}
