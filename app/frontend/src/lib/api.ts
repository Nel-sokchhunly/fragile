import {
  AddNote, AnswerEscalation, CreateSession, DeleteSession, GetAgentEvents, GetRateLimit, GetSession, ListSessions,
  PickDirectory, ResumeSession, SendMessage, StopSession, UpdateNote,
} from '../../wailsjs/go/main/App'
import type {Agent, AgentEvent, ChatItem, Escalation, Note, NoteType, RateLimit, Session, Task} from './types'

// Typed wrappers around the generated Wails bindings (wailsjs/go, regenerate with `wails generate module`
// from app/). The generated typings use classes and plain `string` for enums; the values are plain JSON
// matching lib/types.ts, hence the casts. Every call rejects with the Go error message (string).

// Everything the UI shows for one session; later changes arrive as events (lib/events.ts).
export type SessionSnapshot = {
  session: Session
  agents: Agent[]
  tasks: Task[]
  notes: Note[]
  chat: ChatItem[] // oldest first
  escalations: Escalation[] // the open ones
}

const as = <T>(p: Promise<unknown>) => p as Promise<T>

export const api = {
  /** Native directory chooser; '' if cancelled. */
  pickDirectory: (): Promise<string> => PickDirectory(),
  /** Creates an empty session in `workDir` (must exist); `name` '' = the directory's name. The orchestrator starts with the first sendMessage. */
  createSession: (name: string, workDir: string) => as<Session>(CreateSession(name, workDir)),
  /** Latest subscription limits seen, null until any agent reported them. */
  getRateLimit: () => as<RateLimit | null>(GetRateLimit()),
  /** All sessions, newest first. */
  listSessions: () => as<Session[]>(ListSessions()),
  /** Full state of a session from the database (live or past). */
  getSession: (sessionId: number) => as<SessionSnapshot>(GetSession(sessionId)),
  /** Page an agent's output: events with id > sinceId, oldest first, at most `limit` (default and cap 1000). */
  getAgentEvents: (agentId: number, sinceId = 0, limit = 1000) => as<AgentEvent[]>(GetAgentEvents(agentId, sinceId, limit)),
  /** Chat message to the orchestrator. Rejects if its process is not running (stopped, exited, past session). */
  sendMessage: (sessionId: number, text: string): Promise<void> => SendMessage(sessionId, text),
  /** Answer an open escalation; also delivered to the orchestrator. The chat row updates via chat_item. */
  answerEscalation: (escalationId: number, answer: string): Promise<void> => AnswerEscalation(escalationId, answer),
  /** Post a note as the user (author_agent_id 0). */
  addNote: (sessionId: number, type: NoteType, content: string) => as<Note>(AddNote(sessionId, type, content)),
  /** Edit a note as the user; '' leaves content / status unchanged. */
  updateNote: (sessionId: number, noteId: number, content: string, status: '' | 'open' | 'resolved') =>
    as<Note>(UpdateNote(sessionId, noteId, content, status)),
  /** Stops the session's orchestrator and sub-agents for good. */
  stopSession: (sessionId: number): Promise<void> => StopSession(sessionId),
  /** Starts a new orchestrator that resumes the last one's Claude conversation; rejects if one is running or none ever started. */
  resumeSession: (sessionId: number): Promise<void> => ResumeSession(sessionId),
  /** Stops the session, then removes it and its chat, agents and notes from Fragile (never its files). */
  deleteSession: (sessionId: number): Promise<void> => DeleteSession(sessionId),
}
