import {create} from 'zustand'
import {api} from '@/lib/api'
import type {Agent, AgentEvent, ChatItem, Note, NoteType, RateLimit, Session, SessionStatus, Task} from '@/lib/types'

// Zustand store fed by the backend: snapshots (lib/api.ts) on first view of a session, then Wails events
// (lib/events.ts) routed here by session_id. Components only read it via selectors.

export type SessionData = {agents: Agent[]; tasks: Task[]; notes: Note[]; chat: ChatItem[]}
export type Toast = {id: number; text: string}

type AppState = {
  sessions: Session[]
  selectedSessionId: number | null
  data: Record<number, SessionData> // by session id; present = snapshot loaded
  agentEvents: Record<number, AgentEvent[]> // by agent id; present = history load started (live events then append)
  agentLoaded: Record<number, boolean> // history fully paged in
  lastLine: Record<number, {text: string; at: string}> // newest assistant_text / tool_use per agent, from live events
  selectedAgentId: number | null // null = show the orchestrator chat
  sidebarCollapsed: boolean // mirror of the sidebar panel's collapsed state
  toasts: Toast[]
  limit: RateLimit | null // subscription limits (account-wide)
  confirmDelete: number | null // session awaiting delete confirmation

  init: () => Promise<void>
  selectSession: (id: number | null) => void
  setConfirmDelete: (id: number | null) => void
  setLimit: (l: RateLimit) => void
  selectAgent: (id: number | null) => void
  setSidebarCollapsed: (c: boolean) => void
  notify: (e: unknown) => void
  // Throw the backend's error string; the caller shows it inline.
  createSession: (name: string, workDir: string) => Promise<void>
  sendMessage: (sessionId: number, text: string) => Promise<void>
  // Report failures as toasts.
  stopSession: (sessionId: number) => Promise<void>
  deleteSession: (sessionId: number) => Promise<void> // the session_deleted event removes it
  answerEscalation: (escalationId: number, answer: string) => Promise<void>
  addNote: (sessionId: number, type: NoteType, content: string) => Promise<boolean>
  setNoteStatus: (sessionId: number, noteId: number, status: 'open' | 'resolved') => Promise<void>
  loadAgentEvents: (agentId: number) => Promise<void>

  // Event sinks (lib/events.ts).
  sessionCreated: (s: Session) => void
  sessionStatus: (sessionId: number, status: SessionStatus) => void
  agentSeen: (sessionId: number) => void
  sessionDeleted: (sessionId: number) => void
  patchSession: (sessionId: number, f: (d: SessionData) => SessionData) => void
  agentEvent: (ev: AgentEvent) => void
}

// Insert or replace by id in an id-ordered list (appends and replaces of recent rows are the fast path).
export function upsert<T extends {id: number}>(list: T[], item: T): T[] {
  let i = list.length
  while (i > 0 && list[i - 1].id > item.id) i--
  if (i > 0 && list[i - 1].id === item.id) return [...list.slice(0, i - 1), item, ...list.slice(i)]
  return [...list.slice(0, i), item, ...list.slice(i)]
}

const errText = (e: unknown) => (typeof e === 'string' ? e : e instanceof Error ? e.message : String(e))

// Events for a session whose snapshot is in flight wait here and replay on top of it (idempotent upserts),
// so nothing is lost between "snapshot read" and "snapshot arrived". Events for sessions never opened are
// dropped: opening one fetches everything.
const loading = new Map<number, ((d: SessionData) => SessionData)[]>()
let toastId = 0

export const useAppStore = create<AppState>((set, get) => {
  const loadSession = async (id: number) => {
    if (get().data[id] || loading.has(id)) return
    loading.set(id, [])
    try {
      const {agents, tasks, notes, chat} = await api.getSession(id)
      let d: SessionData = {agents, tasks, notes, chat}
      for (const f of loading.get(id)!) d = f(d)
      set((s) => ({data: {...s.data, [id]: d}}))
    } catch (e) {
      get().notify(e)
    } finally {
      loading.delete(id)
    }
  }
  const select = (id: number | null) => {
    set({selectedSessionId: id, selectedAgentId: null})
    if (id != null) void loadSession(id)
  }
  // Runs an API call whose failure should only surface as a toast.
  const toasting = async <T,>(p: Promise<T>): Promise<T | undefined> => {
    try { return await p } catch (e) { get().notify(e) }
  }

  return {
    sessions: [],
    selectedSessionId: null,
    data: {},
    agentEvents: {},
    agentLoaded: {},
    lastLine: {},
    selectedAgentId: null,
    sidebarCollapsed: false,
    toasts: [],
    limit: null,
    confirmDelete: null,

    init: async () => {
      void api.getRateLimit().then((l) => l && get().setLimit(l), (e) => get().notify(e))
      try {
        const list = await api.listSessions()
        const ids = new Set(list.map((x) => x.id))
        set((s) => ({sessions: [...s.sessions.filter((x) => !ids.has(x.id)), ...list]}))
        if (get().selectedSessionId == null && get().sessions.length) select(get().sessions[0].id)
      } catch (e) {
        get().notify(e)
      }
    },
    selectSession: select,
    setConfirmDelete: (confirmDelete) => set({confirmDelete}),
    setLimit: (limit) => set({limit}),
    selectAgent: (selectedAgentId) => set({selectedAgentId}),
    setSidebarCollapsed: (sidebarCollapsed) => set({sidebarCollapsed}),

    notify: (e) => {
      const id = ++toastId
      set((s) => ({toasts: [...s.toasts, {id, text: errText(e)}]}))
      setTimeout(() => set((s) => ({toasts: s.toasts.filter((t) => t.id !== id)})), 6000)
    },

    createSession: async (name, workDir) => {
      const se = await api.createSession(name, workDir)
      get().sessionCreated(se)
      select(se.id)
    },
    sendMessage: (sid, text) => api.sendMessage(sid, text), // the backend's chat_item event shows the message
    stopSession: async (sid) => { await toasting(api.stopSession(sid)) },
    deleteSession: async (sid) => { await toasting(api.deleteSession(sid)) },
    answerEscalation: async (id, answer) => { await toasting(api.answerEscalation(id, answer)) }, // chat_item flips it to answered
    addNote: async (sid, type, content) => {
      const n = await toasting(api.addNote(sid, type, content))
      if (n) get().patchSession(sid, (d) => ({...d, notes: upsert(d.notes, n)}))
      return !!n
    },
    setNoteStatus: async (sid, noteId, status) => {
      const n = await toasting(api.updateNote(sid, noteId, '', status))
      if (n) get().patchSession(sid, (d) => ({...d, notes: upsert(d.notes, n)}))
    },

    loadAgentEvents: async (agentId) => {
      if (get().agentEvents[agentId]) return
      set((s) => ({agentEvents: {...s.agentEvents, [agentId]: []}})) // live events append from now on
      try {
        let hist: AgentEvent[] = []
        for (;;) {
          const page = await api.getAgentEvents(agentId, hist.length ? hist[hist.length - 1].id : 0, 1000)
          hist = hist.concat(page)
          if (page.length < 1000) break
        }
        set((s) => {
          const seen = new Set(hist.map((e) => e.id))
          const merged = hist.concat(s.agentEvents[agentId].filter((e) => !seen.has(e.id))).sort((a, b) => a.id - b.id)
          return {agentEvents: {...s.agentEvents, [agentId]: merged}, agentLoaded: {...s.agentLoaded, [agentId]: true}}
        })
      } catch (e) {
        set((s) => {
          const {[agentId]: _, ...rest} = s.agentEvents
          return {agentEvents: rest}
        })
        get().notify(e)
      }
    },

    sessionCreated: (se) => set((s) => (s.sessions.some((x) => x.id === se.id) ? s : {sessions: [se, ...s.sessions]})),
    // A status other than done means the orchestrator exists, so the session is no longer new.
    sessionStatus: (sid, status) => set((s) => ({sessions: s.sessions.map((x) => (x.id === sid ? {...x, status, agent_count: x.agent_count || (status === 'done' ? 0 : 1)} : x))})),
    // Any agent row (even a crashed orchestrator) means the session is no longer new.
    agentSeen: (sid) => set((s) => ({sessions: s.sessions.map((x) => (x.id === sid && !x.agent_count ? {...x, agent_count: 1} : x))})),
    sessionDeleted: (sid) => {
      const {sessions, selectedSessionId} = get()
      const i = sessions.findIndex((x) => x.id === sid)
      if (i < 0) return
      const rest = sessions.filter((x) => x.id !== sid)
      // SQLite reuses ids: drop every per-agent cache of the session so a new agent can't inherit its output.
      const {[sid]: gone, ...data} = get().data
      const ids = new Set(gone?.agents.map((a) => a.id))
      const drop = <T,>(m: Record<number, T>) => Object.fromEntries(Object.entries(m).filter(([k]) => !ids.has(+k))) as Record<number, T>
      set((s) => ({
        sessions: rest, data,
        agentEvents: drop(s.agentEvents), agentLoaded: drop(s.agentLoaded), lastLine: drop(s.lastLine),
        selectedAgentId: s.selectedAgentId != null && ids.has(s.selectedAgentId) ? null : s.selectedAgentId,
      }))
      if (selectedSessionId === sid) select(rest[Math.min(i, rest.length - 1)]?.id ?? null) // the next one, else the last
    },
    patchSession: (sid, f) => {
      if (get().data[sid]) set((s) => ({data: {...s.data, [sid]: f(s.data[sid])}}))
      else loading.get(sid)?.push(f)
    },
    agentEvent: (ev) => {
      let text = ''
      try {
        const p = JSON.parse(ev.payload)
        if (ev.event_type === 'assistant_text') text = p.text
        else if (ev.event_type === 'tool_use') text = `${p.name} ${JSON.stringify(p.input ?? {})}`.slice(0, 200)
      } catch { /* not JSON: no line */ }
      set((s) => ({
        lastLine: text ? {...s.lastLine, [ev.agent_id]: {text, at: ev.created_at}} : s.lastLine,
        agentEvents: s.agentEvents[ev.agent_id] ? {...s.agentEvents, [ev.agent_id]: upsert(s.agentEvents[ev.agent_id], ev)} : s.agentEvents,
      }))
    },
  }
})

// Selector helpers. Fallbacks are module constants so selectors return stable references.
export const NO_AGENTS: Agent[] = []
export const NO_TASKS: Task[] = []
export const NO_NOTES: Note[] = []
export const NO_CHAT: ChatItem[] = []
export const NO_EVENTS: AgentEvent[] = []

// The session's current orchestrator process is alive, so it can take messages.
export const orchestratorRunning = (agents: Agent[]) => agents.some((a) => a.role === 'orchestrator' && a.status === 'running')

// One-line "latest output" for an agent card: whichever is newer of the agent's last note and its last
// assistant_text / tool_use event. Returns a string so the card re-renders only when the line changes.
export function latestLine(s: AppState, sessionId: number, agentId: number): string {
  const d = s.data[sessionId]
  const notes = d?.notes ?? NO_NOTES
  let note: Note | undefined
  for (let i = notes.length - 1; i >= 0 && !note; i--) if (notes[i].author_agent_id === agentId) note = notes[i]
  let ev = s.lastLine[agentId]
  if (!ev && d?.agents.find((a) => a.id === agentId)?.role === 'orchestrator') { // past session: no live events, use the chat
    for (let i = d.chat.length - 1; i >= 0 && !ev; i--) {
      const c = d.chat[i]
      if (c.kind === 'assistant') ev = {text: c.text, at: c.at}
      else if (c.kind === 'tool') ev = {text: `${c.name} ${c.summary}`, at: c.at}
    }
  }
  if (note && (!ev || Date.parse(note.updated_at) >= Date.parse(ev.at))) return note.content
  return ev?.text ?? ''
}
