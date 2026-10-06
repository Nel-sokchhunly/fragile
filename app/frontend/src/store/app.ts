import {create} from 'zustand'
import type {Agent, AgentEvent, ChatItem, Escalation, Note, NoteType, Session, Task} from '@/lib/types'
import {mockState} from '@/store/mock'

// Zustand store fed by Wails events (see lib/events.ts); components only read it via selectors.
// Until the Go bindings exist, state is seeded from store/mock.ts and the actions below mutate it locally.

export type SessionData = {agents: Agent[]; tasks: Task[]; notes: Note[]; chat: ChatItem[]}

type AppState = {
  sessions: Session[]
  selectedSessionId: number | null
  data: Record<number, SessionData> // by session id
  agentEvents: Record<number, AgentEvent[]> // by agent id
  selectedAgentId: number | null // null = show the orchestrator chat
  sidebarCollapsed: boolean // mirror of the sidebar panel's collapsed state
  nextId: number // mock id counter

  selectSession: (id: number | null) => void
  selectAgent: (id: number | null) => void
  setSidebarCollapsed: (c: boolean) => void
  createSession: (title: string) => void
  sendUserMessage: (sessionId: number, text: string) => void
  answerEscalation: (sessionId: number, escalationId: number, answer: string) => void
  addNote: (sessionId: number, type: NoteType, content: string) => void
  toggleNoteResolved: (sessionId: number, noteId: number) => void
}

const now = () => new Date().toISOString()

const patchData = (s: AppState, sid: number, f: (d: SessionData) => Partial<SessionData>) =>
  ({data: {...s.data, [sid]: {...s.data[sid], ...f(s.data[sid])}}})

export const useAppStore = create<AppState>((set) => ({
  ...mockState(), // MOCK: replace with `sessions: [], data: {}, ...` when wired to the backend
  selectedAgentId: null,
  sidebarCollapsed: false,

  selectSession: (id) => set({selectedSessionId: id, selectedAgentId: null}),
  selectAgent: (selectedAgentId) => set({selectedAgentId}),
  setSidebarCollapsed: (sidebarCollapsed) => set({sidebarCollapsed}),

  createSession: (title) =>
    set((s) => {
      const id = s.nextId
      return {
        nextId: id + 2,
        sessions: [{id, title, status: 'working', created_at: now()}, ...s.sessions],
        data: {
          ...s.data,
          [id]: {
            agents: [{id: id + 1, session_id: id, role: 'orchestrator', status: 'running', created_at: now()}],
            tasks: [], notes: [], chat: [{id: id + 1, kind: 'user', text: title, at: now()}],
          },
        },
        selectedSessionId: id,
        selectedAgentId: null,
      }
    }),

  sendUserMessage: (sid, text) =>
    set((s) => ({
      nextId: s.nextId + 1,
      ...patchData(s, sid, (d) => ({chat: [...d.chat, {id: s.nextId, kind: 'user', text, at: now()}]})),
    })),

  answerEscalation: (sid, escId, answer) =>
    set((s) => {
      const chat = s.data[sid].chat.map((c) =>
        c.kind === 'escalation' && c.escalation.id === escId
          ? {...c, escalation: {...c.escalation, status: 'answered', answer} as Escalation}
          : c)
      const stillOpen = chat.some((c) => c.kind === 'escalation' && c.escalation.status === 'open')
      return {
        ...patchData(s, sid, () => ({chat})),
        sessions: s.sessions.map((x) => (x.id === sid && !stillOpen ? {...x, status: 'working'} : x)),
      }
    }),

  addNote: (sid, type, content) =>
    set((s) => ({
      nextId: s.nextId + 1,
      ...patchData(s, sid, (d) => ({
        notes: [...d.notes, {id: s.nextId, board_id: sid, author_agent_id: 0, type, content, status: 'open', created_at: now(), updated_at: now()}],
      })),
    })),

  toggleNoteResolved: (sid, noteId) =>
    set((s) => patchData(s, sid, (d) => ({
      notes: d.notes.map((n) => (n.id === noteId ? {...n, status: n.status === 'open' ? 'resolved' : 'open', updated_at: now()} : n)),
    }))),
}))

// Selector helpers. Fallbacks are module constants so selectors return stable references.
export const NO_AGENTS: Agent[] = []
export const NO_TASKS: Task[] = []
export const NO_NOTES: Note[] = []
export const NO_CHAT: ChatItem[] = []
export const NO_EVENTS: AgentEvent[] = []

// One-line "latest output" for an agent card: newest own note, else last assistant text event.
// Returns a string so the card re-renders only when the line actually changes.
export function latestLine(s: AppState, sessionId: number, agentId: number): string {
  const notes = s.data[sessionId]?.notes ?? NO_NOTES
  for (let i = notes.length - 1; i >= 0; i--) if (notes[i].author_agent_id === agentId) return notes[i].content
  const ev = s.agentEvents[agentId]
  for (let i = (ev?.length ?? 0) - 1; i >= 0; i--) {
    if (ev[i].event_type !== 'assistant_text') continue
    try { return (JSON.parse(ev[i].payload) as {text: string}).text } catch { return '' }
  }
  return ''
}
