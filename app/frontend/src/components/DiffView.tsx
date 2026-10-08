import {type CSSProperties, type Ref, memo, useEffect, useMemo, useRef, useState} from 'react'
import {GroupedVirtuoso, type GroupedVirtuosoHandle, type ListRange} from 'react-virtuoso'
import {ChevronRight} from 'lucide-react'
import {type Token, tokenize} from '@/lib/shiki'
import type {ChangedFile, ChangeStatus, DiffLine, FileDiff} from '@/lib/types'
import {cn} from '@/lib/utils'

// Zed-style multibuffer diff: every changed file stacked in one virtualized list, one row per line (or gap / notice)
// under a sticky, collapsible file header. Only hunks show; the unchanged lines around them fold into "⋯ N lines"
// rows that expand from file_lines.

export const STATUS_STYLE: Record<ChangeStatus, string> = {
  A: 'text-status-working', '?': 'text-status-working', D: 'text-status-crashed', M: 'text-status-needs-you', R: 'text-status-decision',
}
export const StatusLetter = ({s}: {s: ChangeStatus}) => <span className={cn('w-2.5 shrink-0 text-center font-mono text-xs font-semibold', STATUS_STYLE[s])}>{s}</span>
export const Counts = ({added, removed}: {added: number; removed: number}) => (
  <span className="shrink-0 font-mono text-xs tabular-nums">
    {added > 0 && <span className="text-diff-added">+{added}</span>}{added > 0 && removed > 0 && ' '}
    {removed > 0 && <span className="text-diff-removed">-{removed}</span>}
  </span>
)
// "dir/" muted, then the file name.
export function PathLabel({path}: {path: string}) {
  const i = path.lastIndexOf('/') + 1
  return <><span className="text-muted-foreground">{path.slice(0, i)}</span>{path.slice(i)}</>
}

// What the panel has for a file: the diff for `sig`, or why there is none.
export type DiffEntry = {sig: string; diff?: FileDiff; error?: string}

type Row =
  | {kind: 'line'; file: number; line: DiffLine; nw: number}
  | {kind: 'gap'; file: number; id: string; start: number; end: number; offset: number; nw: number} // new-side lines [start, end); old = new + offset
  | {kind: 'info'; file: number; text: string}

// New-side line numbers [start, end) that a hunk spans; a hunk with no lines on a side sits after its *_start line.
const span = (start: number, n: number) => (n ? start : start + 1)

function fileRows(file: number, f: ChangedFile, e: DiffEntry | undefined, open: ReadonlySet<string>): Row[] {
  const d = e?.diff
  if (f.binary || d?.binary) return [{kind: 'info', file, text: 'binary'}]
  if (e?.error && !d) return [{kind: 'info', file, text: `diff unavailable: ${e.error}`}]
  if (!d) return [{kind: 'info', file, text: 'Loading...'}]
  if (d.too_large) return [{kind: 'info', file, text: 'too large to display'}]
  if (!d.hunks.length) return [{kind: 'info', file, text: 'no content changes'}]
  const last = d.hunks[d.hunks.length - 1]
  const nw = String(Math.max(d.file_lines.length, last.old_start + last.old_lines, last.new_start + last.new_lines)).length
  const rows: Row[] = []
  const gap = (start: number, end: number, offset: number) => {
    if (end <= start) return
    const id = `${f.path}\0${start}`
    if (!open.has(id)) { rows.push({kind: 'gap', file, id, start, end, offset, nw}); return }
    for (let n = start; n < end; n++) rows.push({kind: 'line', file, nw, line: {kind: ' ', text: d.file_lines[n - 1] ?? '', old_no: n + offset, new_no: n}})
  }
  let next = 1 // first new-side line after the previous hunk
  let offset = 0
  for (const h of d.hunks) {
    const start = span(h.new_start, h.new_lines)
    const oldStart = span(h.old_start, h.old_lines)
    gap(next, start, oldStart - start)
    for (const line of h.lines) rows.push({kind: 'line', file, line, nw})
    next = start + h.new_lines
    offset = oldStart + h.old_lines - next
  }
  gap(next, d.file_lines.length + 1, offset)
  return rows
}

// Highlighting: new side from file_lines (by new_no), old side from the '-' lines (by old_no). null = plain text.
type FileTokens = {new: Token[][] | null; old: Map<number, Token[]> | null}
const PLAIN: FileTokens = {new: null, old: null}
const MAX_HL_LINES = 5000 // ponytail: bigger files stay plain; highlight only the visible slice if that matters
const ALIAS: Record<string, string> = {mjs: 'js', cjs: 'js', mts: 'ts', cts: 'ts', h: 'c', hpp: 'cpp', cc: 'cpp', htm: 'html', svg: 'xml'}
function langOf(path: string) {
  const base = path.slice(path.lastIndexOf('/') + 1).toLowerCase()
  const ext = base.slice(base.lastIndexOf('.') + 1) // no dot: the whole name (Dockerfile, Makefile)
  return ALIAS[ext] ?? ext
}
async function fileTokens(d: FileDiff): Promise<FileTokens> {
  const lang = langOf(d.path)
  const removed = d.hunks.flatMap((h) => h.lines.filter((l) => l.kind === '-'))
  const side = (lines: string[]) => (lines.length && lines.length <= MAX_HL_LINES ? tokenize(lines.join('\n'), lang).catch(() => null) : null)
  const [n, o] = await Promise.all([side(d.file_lines), side(removed.map((l) => l.text))])
  return {new: n, old: o && new Map<number, Token[]>(removed.map((l, i) => [l.old_no, o[i] ?? []]))}
}

const LineRow = memo(function LineRow({line, nw, tokens}: {line: DiffLine; nw: number; tokens?: Token[]}) {
  const {kind} = line
  return (
    <div
      style={{'--nw': `${nw + 1}ch`} as CSSProperties}
      className={cn('grid min-h-[18px] grid-cols-[2px_var(--nw)_var(--nw)_minmax(0,1fr)] font-mono text-xs leading-[18px]', kind === '+' && 'bg-diff-added/12', kind === '-' && 'bg-diff-removed/12')}
    >
      <span className={cn(kind === '+' && 'bg-diff-added', kind === '-' && 'bg-diff-removed')}/>
      <span className="pr-1 text-right text-muted-foreground/50 select-none">{line.old_no || ''}</span>
      <span className="pr-1 text-right text-muted-foreground/50 select-none">{line.new_no || ''}</span>
      <span className="shiki pr-2 pl-2 whitespace-pre-wrap [overflow-wrap:anywhere]">
        {tokens ? tokens.map((t, i) => <span key={i} style={t.htmlStyle as CSSProperties}>{t.content}</span>) : line.text}
      </span>
    </div>
  )
})

function FileHeader({f, closed, onToggle}: {f: ChangedFile; closed: boolean; onToggle: () => void}) {
  return (
    <button
      type="button" onClick={onToggle} aria-expanded={!closed} title={f.old_path ? `${f.old_path} → ${f.path}` : f.path}
      className="flex h-7 w-full cursor-pointer items-center gap-1.5 border-y border-border-default bg-secondary pr-2 pl-1 text-left text-xs hover:bg-surface-hover"
    >
      <ChevronRight className={cn('size-3.5 shrink-0 text-muted-foreground transition-transform', !closed && 'rotate-90')}/>
      <StatusLetter s={f.status}/>
      <span className="min-w-0 flex-1 truncate font-mono"><PathLabel path={f.path}/></span>
      {f.binary ? <span className="shrink-0 text-muted-foreground">binary</span> : <Counts {...f}/>}
    </button>
  )
}

export function DiffView({files, diffs, closed, onToggle, listRef}: {
  files: ChangedFile[]; diffs: Record<string, DiffEntry>; closed: boolean[]; onToggle: (path: string) => void
  listRef: Ref<GroupedVirtuosoHandle>
}) {
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set()) // expanded gaps
  const perFile = useMemo(() => files.map((f, i) => (closed[i] ? [] : fileRows(i, f, diffs[f.path], open))), [files, diffs, closed, open])
  const rows = useMemo(() => perFile.flat(), [perFile])
  const counts = useMemo(() => perFile.map((r) => r.length), [perFile])

  // Highlight lazily: only files with rows on screen, one at a time. Keyed by diff object, so a refetch re-highlights.
  const [tokens, setTokens] = useState(() => new Map<FileDiff, FileTokens>())
  const [range, setRange] = useState<ListRange>({startIndex: 0, endIndex: 0})
  const started = useRef(new WeakSet<FileDiff>())
  const queue = useRef(Promise.resolve())
  const latest = useRef(diffs)
  latest.current = diffs
  useEffect(() => {
    const seen = new Set<number>()
    for (let i = range.startIndex; i <= Math.min(range.endIndex, rows.length - 1); i++) seen.add(rows[i].file)
    for (const fi of seen) {
      const d = diffs[files[fi].path]?.diff
      if (!d || d.binary || d.too_large || started.current.has(d)) continue
      started.current.add(d)
      queue.current = queue.current.then(async () => {
        const t = await fileTokens(d).catch(() => PLAIN)
        const live = new Set(Object.values(latest.current).map((e) => e.diff))
        setTokens((m) => new Map([...m].filter(([k]) => live.has(k))).set(d, t))
        await new Promise((r) => setTimeout(r)) // let a frame through between files
      })
    }
  }, [range, rows, files, diffs])

  return (
    <GroupedVirtuoso
      ref={listRef}
      groupCounts={counts}
      increaseViewportBy={300}
      rangeChanged={setRange}
      groupContent={(gi) => <FileHeader f={files[gi]} closed={closed[gi]} onToggle={() => onToggle(files[gi].path)}/>}
      itemContent={(i) => {
        const r = rows[i]
        if (!r) return null
        if (r.kind === 'info') return <div className="py-1 pl-6 text-xs text-muted-foreground">{r.text}</div>
        if (r.kind === 'gap') {
          const n = r.end - r.start
          return (
            <button
              type="button" onClick={() => setOpen((s) => new Set(s).add(r.id))}
              style={{paddingLeft: `calc(2px + ${2 * (r.nw + 1)}ch + 0.5rem)`}}
              className="flex h-[18px] w-full cursor-pointer items-center bg-muted/60 font-mono text-xs text-muted-foreground hover:text-foreground"
            >
              ⋯ {n} {n === 1 ? 'line' : 'lines'}
            </button>
          )
        }
        const t = tokens.get(diffs[files[r.file].path]?.diff as FileDiff)
        const lt = r.line.kind === '-' ? t?.old?.get(r.line.old_no) : t?.new?.[r.line.new_no - 1]
        return <LineRow line={r.line} nw={r.nw} tokens={lt}/>
      }}
      className="absolute inset-0"
    />
  )
}
