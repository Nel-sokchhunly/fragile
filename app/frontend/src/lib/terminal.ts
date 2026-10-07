import {Terminal} from '@xterm/xterm'
import {FitAddon} from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import {api} from '@/lib/api'
import {on} from '@/lib/events'

// One xterm per session, kept here (not in React) so hiding the pane or switching sessions keeps the screen
// and scrollback. Each lives in its own div that TerminalPane appends into its container and detaches again.
// Output events are written whether or not the pane is shown.

type Entry = {
  term: Terminal
  fit: FitAddon
  el: HTMLDivElement
  exited: boolean // the shell ended; Enter starts a new one
  opening: Uint8Array[] | null // output that arrived during the first open, while the backlog is in flight
  resizeTimer?: ReturnType<typeof setTimeout>
}

const registry = new Map<number, Entry>()

const decode = (b64: string) => Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))
const errText = (e: unknown) => (typeof e === 'string' ? e : e instanceof Error ? e.message : String(e))

// Ctrl/Cmd+` toggles the pane (ChatView); xterm must let it bubble instead of sending it to the shell.
export const isToggleKey = (e: KeyboardEvent) => (e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.code === 'Backquote'

function create(sid: number): Entry {
  const mono = getComputedStyle(document.documentElement).getPropertyValue('--font-mono').trim()
  const term = new Terminal({
    fontFamily: mono || "'JetBrains Mono Variable', ui-monospace, Menlo, monospace",
    fontSize: 13,
    cursorBlink: true,
    scrollback: 5000,
    theme: {background: '#1f1f1e', foreground: '#f2f1ec', cursor: '#f2f1ec', cursorAccent: '#1f1f1e', selectionBackground: 'rgba(242, 241, 236, 0.25)'},
  })
  const fit = new FitAddon()
  term.loadAddon(fit)
  const el = document.createElement('div')
  el.style.height = '100%'
  const e: Entry = {term, fit, el, exited: false, opening: null}
  term.attachCustomKeyEventHandler((ev) => !isToggleKey(ev))
  term.onData((data) => {
    if (e.exited) {
      if (data === '\r') restart(sid, e)
      return
    }
    api.terminalWrite(sid, data).catch(() => { /* shell gone: terminal_exit says so */ })
  })
  term.onResize(({cols, rows}) => {
    clearTimeout(e.resizeTimer)
    e.resizeTimer = setTimeout(() => { api.terminalResize(sid, cols, rows).catch(() => {}) }, 100)
  })
  registry.set(sid, e)
  return e
}

function restart(sid: number, e: Entry) {
  e.exited = false
  e.term.write('\r\n')
  // A new shell's output arrives as events; its backlog would only repeat it.
  api.terminalOpen(sid, e.term.cols, e.term.rows).catch((err) => failed(e, err))
}

function failed(e: Entry, err: unknown) {
  e.exited = true
  e.term.write(`\r\n\x1b[31m${errText(err)}\x1b[0m\r\n\x1b[2mpress Enter to retry\x1b[0m\r\n`)
}

// Fit to the container, skipping while it has no size (not laid out yet).
function fitNow(e: Entry) {
  if (e.el.clientWidth > 0 && e.el.clientHeight > 0) e.fit.fit()
}

// Shows the session's terminal in `container`, starting the shell on first use. Returns the detach function.
export function attachTerminal(sid: number, container: HTMLElement): () => void {
  let e = registry.get(sid)
  const fresh = !e
  if (!e) e = create(sid)
  const entry = e
  container.appendChild(entry.el)
  if (fresh) {
    entry.term.open(entry.el)
    // The web font may still be loading; measure again once it is in.
    void document.fonts?.ready.then(() => { if (entry.el.isConnected) fitNow(entry) })
  }
  fitNow(entry)
  entry.term.focus()

  if (fresh) {
    entry.opening = []
    api.terminalOpen(sid, entry.term.cols, entry.term.rows).then((backlog) => {
      if (!entry.opening) return // terminal_exit came first and flushed it
      const live = entry.opening
      entry.opening = null
      // Output already arriving means the shell just started and events carry it; else repaint from the backlog
      // (the shell outlived an earlier frontend, e.g. after a reload).
      if (live.length) live.forEach((b) => entry.term.write(b))
      else if (backlog) entry.term.write(decode(backlog))
    }, (err) => {
      const live = entry.opening ?? []
      entry.opening = null
      live.forEach((b) => entry.term.write(b))
      failed(entry, err)
    })
  } else if (!entry.exited) {
    // Already running: this only resizes it (the instance kept its screen, so the backlog is ignored).
    api.terminalOpen(sid, entry.term.cols, entry.term.rows).catch((err) => failed(entry, err))
  }

  let raf = 0
  const ro = new ResizeObserver(() => {
    cancelAnimationFrame(raf)
    raf = requestAnimationFrame(() => fitNow(entry))
  })
  ro.observe(container)
  return () => {
    ro.disconnect()
    cancelAnimationFrame(raf)
    entry.el.remove()
  }
}

export function focusTerminal(sid: number) {
  registry.get(sid)?.term.focus()
}

function dispose(sid: number) {
  const e = registry.get(sid)
  if (!e) return
  clearTimeout(e.resizeTimer)
  e.term.dispose()
  e.el.remove()
  registry.delete(sid)
}

// Call once at startup. Output for sessions whose terminal was never shown is dropped (the backlog covers it).
export function subscribeTerminal() {
  const offs = [
    on('terminal_output', (ev) => {
      const e = registry.get(ev.session_id)
      if (!e) return
      const bytes = decode(ev.payload.data)
      if (e.opening) e.opening.push(bytes)
      else e.term.write(bytes)
    }),
    on('terminal_exit', (ev) => {
      const e = registry.get(ev.session_id)
      if (!e) return
      e.opening?.forEach((b) => e.term.write(b))
      e.opening = null
      e.exited = true
      e.term.write(`\r\n\x1b[2m[process exited ${ev.payload.code}] press Enter to restart\x1b[0m\r\n`)
    }),
    // Session ids get reused, so a deleted session's terminal must not survive.
    on('session_deleted', (ev) => dispose(ev.session_id)),
  ]
  return () => offs.forEach((off) => off())
}
