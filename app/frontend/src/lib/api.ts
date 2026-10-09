import {
  AddNote, AnswerEscalation, CompactSession, CreateSessionWithProvider, DeleteSession, GetAgentEventTail, GetAgentEvents, GetAttachment, GetChanges, GetFileDiff, GetProviders, GetRateLimit, GetSession, GetSettings,
  FinishAgent, InterruptSession, ListRepos, ListSessions, PauseAgent, PickDirectory, ResumeAgent, ResumeSession, SendMessage, SetSessionConfig, SetSettings, StopSession, TerminalClose, TerminalOpen,
  TerminalResize, TerminalWrite, UpdateNote,
} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import type {Agent, AgentActivity, AgentEvent, Attachment, Changes, ChatItem, Escalation, FileDiff, Note, NoteType, ProviderInfo, RateLimit, Session, SessionConfig, SessionProvider, Settings, Task} from './types'

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
  activity?: Record<number, AgentActivity> // running sub-agents by agent id; null/absent = none
}

const as = <T>(p: Promise<unknown>) => p as Promise<T>

export const api = {
  /** Native directory chooser; '' if cancelled. */
  pickDirectory: (): Promise<string> => PickDirectory(),
  /** Creates an empty session in `workDir` (must exist); `name` '' = the directory's name. The orchestrator starts with the first sendMessage. */
  createSession: (name: string, workDir: string, provider: SessionProvider, cfg: SessionConfig) => as<Session>(CreateSessionWithProvider(name, workDir, provider, cfg)),
  /** Replaces the session's own settings; a changed CLI list or rules reach its orchestrator as a decision note. */
  setSessionConfig: (sessionId: number, cfg: SessionConfig) => as<Session>(SetSessionConfig(sessionId, cfg)),
  /** Detects each sub-agent CLI (runs their --version; call on demand). */
  getProviders: () => as<ProviderInfo[]>(GetProviders()),
  /** Latest subscription limits seen, null until any agent reported them. */
  getRateLimit: () => as<RateLimit | null>(GetRateLimit()),
  /** User preferences, with defaults for anything never set. */
  getSettings: () => as<Settings>(GetSettings()),
  /** Validates and stores the preferences; rejects with the reason (e.g. an auto-compact threshold out of range). */
  setSettings: (s: Settings): Promise<void> => SetSettings(main.Settings.createFrom(s)),
  /** All sessions, newest first. */
  listSessions: () => as<Session[]>(ListSessions()),
  /** Full state of a session from the database (live or past). */
  getSession: (sessionId: number) => as<SessionSnapshot>(GetSession(sessionId)),
  /** Page an agent's output: events with id > sinceId, oldest first, at most `limit` (default and cap 1000). */
  getAgentEvents: (agentId: number, sinceId = 0, limit = 1000) => as<AgentEvent[]>(GetAgentEvents(agentId, sinceId, limit)),
  /** Newest bounded output in chronological order (default and cap 2000). */
  getAgentEventTail: (agentId: number, limit = 2000) => as<AgentEvent[]>(GetAgentEventTail(agentId, limit)),
  /** Chat message to the orchestrator; text may be '' if there are attachments. Rejects if its process is not
   *  running (stopped, exited, past session) or an attachment is invalid. */
  sendMessage: (sessionId: number, text: string, attachments: Attachment[] = []): Promise<void> => SendMessage(sessionId, text, attachments),
  /** Cancels the orchestrator's current turn (process, conversation and sub-agents stay); no-op if idle. */
  interruptSession: (sessionId: number): Promise<void> => InterruptSession(sessionId),
  /** Sends /compact to the orchestrator; rejects if it is not running or mid-turn. The result shows as a notice chat item. */
  compactSession: (sessionId: number): Promise<void> => CompactSession(sessionId),
  /** Sub-agent: ends its current turn and holds it (no automatic wakes) until resumeAgent. */
  pauseAgent: (agentId: number): Promise<void> => PauseAgent(agentId),
  /** Sub-agent: continues a paused or idle one; relaunches one that stopped without a done note. */
  resumeAgent: (agentId: number): Promise<void> => ResumeAgent(agentId),
  /** Sub-agent: ends it (recorded as stopped; the orchestrator is told). */
  finishAgent: (agentId: number): Promise<void> => FinishAgent(agentId),
  /** Data URL ("data:<type>;base64,...") of attachment `index` of the user chat item `chatItemId`. */
  getAttachment: (sessionId: number, chatItemId: number, index: number): Promise<string> => GetAttachment(sessionId, chatItemId, index),
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

  // Uncommitted git changes in the session's work dir (read-only).
  /** Repo dirs relative to the work dir: [""] if the work dir is a repo, else its child repos; [] if none. */
  listRepos: (sessionId: number) => as<string[] | null>(ListRepos(sessionId)).then((r) => r ?? []),
  /** Changed and untracked files, sorted by path; is_repo false (no error) outside a git repo. `repo` is a listRepos entry. */
  // Go nil slices arrive as null; these hand back [] instead.
  getChanges: (sessionId: number, repo: string) => as<Changes>(GetChanges(sessionId, repo)).then((c) => ({...c, files: c.files ?? []})),
  /** -U3 hunks plus the whole working-tree file; `path` must be one of getChanges' files. */
  getFileDiff: (sessionId: number, repo: string, path: string) => as<FileDiff>(GetFileDiff(sessionId, repo, path)).then((d) => ({
    ...d, file_lines: d.file_lines ?? [], hunks: (d.hunks ?? []).map((h) => ({...h, lines: h.lines ?? []})),
  })),

  // The session's shell (one per session, in its work dir). Output arrives as terminal_output events.
  /** Starts the shell if none is running (else resizes it); resolves with the base64 backlog of recent output. */
  terminalOpen: (sessionId: number, cols: number, rows: number): Promise<string> => TerminalOpen(sessionId, cols, rows),
  /** Input exactly as xterm's onData gives it. */
  terminalWrite: (sessionId: number, data: string): Promise<void> => TerminalWrite(sessionId, data),
  terminalResize: (sessionId: number, cols: number, rows: number): Promise<void> => TerminalResize(sessionId, cols, rows),
  /** Kills the shell; idempotent. */
  terminalClose: (sessionId: number): Promise<void> => TerminalClose(sessionId),
}
