import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import type {GroupedVirtuosoHandle} from 'react-virtuoso'
import {RefreshCw} from 'lucide-react'
import {Button} from '@/components/ui/button'
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue} from '@/components/ui/select'
import {Tooltip, TooltipContent, TooltipTrigger} from '@/components/ui/tooltip'
import {Counts, DiffView, type DiffEntry, PathLabel, StatusLetter} from '@/components/DiffView'
import {api} from '@/lib/api'
import type {Changes} from '@/lib/types'
import {cn} from '@/lib/utils'
import {useAppStore} from '@/store/app'

// Code changes: the session work dir's uncommitted changes (git diff HEAD + untracked). Mounted only while shown and
// keyed by session, so polling stops when it is hidden or the session changes. Polls every POLL_MS only while agents
// work (an orchestrator turn or a running sub-agent), refreshes once when they stop, when a `done` note lands, and on
// the button; a file's diff is refetched only when its sig changed. Old data stays up
// while new data loads, so nothing flashes.

const POLL_MS = 3000
const MAX_OPEN_LINES = 1000 // files with more diff lines start collapsed
const CONCURRENT_DIFFS = 6

export function ChangesPanel({sessionId}: {sessionId: number}) {
  const [changes, setChanges] = useState<Changes | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [diffs, setDiffs] = useState<Record<string, DiffEntry>>({})
  const [toggled, setToggled] = useState<Record<string, boolean>>({}) // the user's collapse choice per path
  const [repos, setRepos] = useState<string[]>([])
  const [repo, setRepo] = useState<string | null>(null) // null until listRepos answered; "" = the work dir itself
  const repoRef = useRef<string | null>(null)
  const list = useRef<GroupedVirtuosoHandle>(null)
  const alive = useRef(true)
  const busy = useRef(false)
  const fetching = useRef(new Set<string>())
  const latest = useRef(changes)
  latest.current = changes

  const switchTo = useCallback((r: string) => {
    repoRef.current = r
    setRepo(r)
    setChanges(null)
    setDiffs({})
    setToggled({})
    setError('')
    fetching.current.clear()
  }, [])

  // relist: re-run listRepos first (on open and on the button; polls reuse the known list).
  const refresh = useCallback(async (relist = false) => {
    if (busy.current) return
    busy.current = true
    setLoading(true)
    try {
      let cur = repoRef.current
      if (relist || cur === null) {
        const rs = await api.listRepos(sessionId)
        if (!alive.current) return
        setRepos(rs)
        const stored = useAppStore.getState().changesRepo[sessionId]
        const pick = rs.length === 0 ? null : rs.includes(stored) ? stored : rs[0]
        if (pick !== cur) switchTo(pick ?? '')
        if (pick === null) { // no repo anywhere
          setError('')
          setChanges({is_repo: false, files: []})
          return
        }
        cur = pick
      }
      if (cur === null) return
      const c = await api.getChanges(sessionId, cur)
      if (!alive.current || repoRef.current !== cur) return
      setError('')
      setChanges((p) => (p && JSON.stringify(p) === JSON.stringify(c) ? p : c))
      const paths = new Set(c.files.map((f) => f.path))
      setDiffs((d) => (Object.keys(d).every((p) => paths.has(p)) ? d : Object.fromEntries(Object.entries(d).filter(([p]) => paths.has(p)))))
    } catch (e) {
      if (alive.current) setError(String(e))
    } finally {
      busy.current = false
      if (alive.current) setLoading(false)
    }
  }, [sessionId, switchTo])
  useEffect(() => {
    alive.current = true
    return () => { alive.current = false }
  }, [])
  const select = (r: string) => {
    useAppStore.getState().setChangesRepo(sessionId, r)
    switchTo(r)
    busy.current = false // an in-flight reply for the old repo is dropped by its repo check
    void refresh()
  }
  const working = useAppStore((s) => (s.busy[sessionId] ?? 0) > 0
    || !!s.data[sessionId]?.agents.some((a) => a.role !== 'orchestrator' && a.status === 'running'))
  useEffect(() => {
    void refresh(true) // on open, and once more when agents stop (their last edits)
    if (!working) return
    const t = setInterval(() => void refresh(), POLL_MS)
    return () => clearInterval(t)
  }, [working, refresh])
  // A sub-agent finishing usually means files changed.
  const lastDone = useAppStore((s) => s.data[sessionId]?.notes.findLast((n) => n.type === 'done')?.id ?? 0)
  useEffect(() => { if (lastDone) void refresh() }, [lastDone, refresh])

  const files = changes?.files
  const closed = useMemo(() => (files ?? []).map((f) =>
    toggled[f.path] ?? (f.binary || f.added + f.removed > MAX_OPEN_LINES || !!diffs[f.path]?.diff?.too_large)), [files, toggled, diffs])

  // Fetch diffs of open files whose sig moved. A reply for a sig that is no longer current is dropped.
  useEffect(() => {
    if (repo === null) return
    const need = (files ?? []).filter((f, i) => !closed[i] && !f.binary && diffs[f.path]?.sig !== f.sig && !fetching.current.has(`${f.path}\0${f.sig}`))
    if (!need.length) return
    void (async () => {
      for (let i = 0; i < need.length; i += CONCURRENT_DIFFS) {
        await Promise.all(need.slice(i, i + CONCURRENT_DIFFS).map(async (f) => {
          const key = `${f.path}\0${f.sig}`
          fetching.current.add(key)
          let e: DiffEntry
          try { e = {sig: f.sig, diff: await api.getFileDiff(sessionId, repo, f.path)} } catch (err) { e = {sig: f.sig, error: String(err)} }
          fetching.current.delete(key)
          if (!alive.current || repoRef.current !== repo || latest.current?.files.find((x) => x.path === f.path)?.sig !== f.sig) return
          setDiffs((d) => ({...d, [f.path]: e}))
        }))
      }
    })()
  }, [files, closed, diffs, sessionId, repo])

  const toggle = useCallback((path: string) => {
    const i = latest.current?.files.findIndex((f) => f.path === path) ?? -1
    if (i >= 0) setToggled((t) => ({...t, [path]: !closed[i]}))
  }, [closed])
  const total = (files ?? []).reduce((s, f) => ({added: s.added + f.added, removed: s.removed + f.removed}), {added: 0, removed: 0})

  return (
    <section className="flex h-full min-h-0 flex-col" aria-label="Code changes">
      <header className="flex h-9 shrink-0 items-center gap-2 pr-1.5 pl-3">
        <h2 className="text-title font-semibold">Changes</h2>
        {repos.length > 1 && repo !== null && (
          <Select value={repo} onValueChange={select}>
            <SelectTrigger size="sm" aria-label="Repository" className="h-6 min-w-0 max-w-[40%] shrink gap-1 px-1.5 py-0 font-mono text-xs">
              <SelectValue className="truncate"/>
            </SelectTrigger>
            <SelectContent>
              {repos.map((r) => <SelectItem key={r} value={r} className="font-mono text-xs">{r}</SelectItem>)}
            </SelectContent>
          </Select>
        )}
        {files && files.length > 0 && (
          <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground">
            {files.length} {files.length === 1 ? 'file' : 'files'} <Counts {...total}/>
          </span>
        )}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-sm" className="ml-auto" onClick={() => void refresh(true)} aria-label="Refresh changes">
              <RefreshCw className={cn(loading && 'animate-spin')}/>
            </Button>
          </TooltipTrigger>
          <TooltipContent>Refresh</TooltipContent>
        </Tooltip>
      </header>
      {error && <p role="alert" className="shrink-0 px-3 pb-1 text-[13px] break-words text-destructive">{error}</p>}
      {!changes ? (
        !error && <p className="px-3 text-[13px] text-muted-foreground">Loading...</p>
      ) : !changes.is_repo ? (
        <p className="px-3 text-[13px] text-muted-foreground">Not a git repository</p>
      ) : !changes.files.length ? (
        <p className="px-3 text-[13px] text-muted-foreground">No uncommitted changes</p>
      ) : (
        <>
          <ul className="max-h-[30%] shrink-0 overflow-y-auto border-b pb-1">
            {changes.files.map((f, i) => (
              <li key={f.path}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      onClick={() => list.current?.scrollToIndex({groupIndex: i, align: 'start'})}
                      className="flex w-full cursor-pointer items-center gap-1.5 px-3 py-px text-left text-[13px] leading-[18px] hover:bg-surface-hover"
                    >
                      <StatusLetter s={f.status}/>
                      <span className="min-w-0 flex-1 truncate font-mono text-xs"><PathLabel path={f.path}/></span>
                      {f.binary ? <span className="shrink-0 text-xs text-muted-foreground">binary</span> : <Counts {...f}/>}
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom" className="font-mono break-all">{f.old_path ? `${f.old_path} → ${f.path}` : f.path}</TooltipContent>
                </Tooltip>
              </li>
            ))}
          </ul>
          <div className="relative min-h-0 flex-1">
            <DiffView files={changes.files} diffs={diffs} closed={closed} onToggle={toggle} listRef={list}/>
          </div>
        </>
      )}
    </section>
  )
}
