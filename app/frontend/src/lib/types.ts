// TS mirrors of the Go structs in notes/store.go (JSON tags). Timestamps are RFC 3339 strings.
// Go `omitempty` ids/strings are optional here; 0 / absent means "none".

export type SessionStatus = 'working' | 'done' | 'needs_you'
export type AgentRole = 'orchestrator' | 'subagent'
export type AgentStatus = 'running' | 'exited' | 'stopped' | 'crashed'
export type TaskStatus = 'planned' | 'working' | 'blocked' | 'review' | 'done'
export type NoteType = 'decision' | 'blocker' | 'heads_up' | 'done' | 'question'
export type NoteStatus = 'open' | 'resolved'
export type EscalationStatus = 'open' | 'answered'

export type SessionProvider = 'claude' | 'codex' | 'agy'

export const PROVIDER_NAMES: Record<SessionProvider, string> = {
  claude: 'Claude',
  codex: 'Codex',
  agy: 'Antigravity',
}

export const PROVIDERS: SessionProvider[] = ['claude', 'codex', 'agy']

// A CLI's detection result (Go: notes.ProviderInfo); reason says how to fix an unavailable one.
export type ProviderInfo = {name: SessionProvider; available: boolean; reason?: string}

// A session's own settings (Go: notes.SessionConfig), copied from the Settings template on create.
// enabled_providers: the CLIs sub-agents may run on, first = default. auto_compact_tokens: 0 = off.
export type SessionConfig = {enabled_providers: SessionProvider[]; auto_compact_tokens: number; orchestrator_rules: string}

export type Session = {
  provider: SessionProvider
  id: number
  title: string
  status: SessionStatus
  work_dir?: string // directory its agents run in; absent/'' for sessions without one
  created_at: string
  agent_count: number // 0 = new: the orchestrator starts with the first message
} & SessionConfig

export type Agent = {
  id: number
  session_id: number
  parent_id?: number
  role: AgentRole
  task_id?: number
  status: AgentStatus
  pid?: number
  log_path?: string
  exit_code?: number
  created_at: string
  exited_at?: string
  context_used?: number // tokens in the agent's latest message; absent = unknown
  context_window?: number
  model?: string // e.g. "claude-opus-5-5", from the agent's init line; absent = unknown
  provider: SessionProvider
}

// Runtime state of a running sub-agent (Go: AgentActivity): mid-turn, and paused by the user (no automatic wakes).
export type AgentActivity = {busy: boolean; paused: boolean}

// Account-wide subscription limits; utilization 0..1, resets_at unix seconds. null window = not reported yet.
export type LimitWindow = {utilization: number; resets_at: number}
export type RateLimit = {five_hour: LimitWindow | null; seven_day: LimitWindow | null}

// User preferences (Go: Settings): the template new sessions copy, plus each CLI's default model.
// auto_compact_tokens: 0 = off, else 20000..1000000.
export type Settings = {
  auto_compact_tokens: number
  orchestrator_rules: string
  subagent_providers: Partial<Record<SessionProvider, {enabled: boolean; default_model?: string}>>
  /** Plugin names newly spawned sub-agents do not load (global, not per session). */
  subagent_disabled_plugins: string[] | null
}

export type Task = {
  id: number
  session_id: number
  title: string
  description: string
  status: TaskStatus
  agent_id?: number
}

// author_agent_id 0 = the user (backend: NULL author in the database, 0 in JSON).
export type Note = {
  id: number
  board_id: number
  author_agent_id: number
  type: NoteType
  content: string
  status: NoteStatus
  created_at: string
  updated_at: string
}

export type Escalation = {
  id: number
  session_id: number
  agent_id: number
  question: string
  context: string
  status: EscalationStatus
  answer?: string
}

// payload is caller-defined JSON text. The UI currently expects (see AgentOutputView):
//   assistant_text {text}, tool_use {name, input}, tool_result {content, is_error?}
export type AgentEvent = {
  id: number
  agent_id: number
  event_type: string
  payload: string
  created_at: string
}

// A file sent with a chat message (Go: Attachment). data is base64 of the bytes, no data: prefix.
export type Attachment = {name: string; media_type: string; data: string}
// What the chat keeps of a sent attachment (Go: AttachmentInfo); size in bytes. Fetch it with api.getAttachment.
export type AttachmentInfo = {name: string; media_type: string; size: number}

// Uncommitted changes in a session's work dir (Go: app/changes.go). status "?" = untracked.
// sig changes whenever the file's diff may have, so the UI refetches only those.
export type ChangeStatus = 'M' | 'A' | 'D' | 'R' | '?'
export type ChangedFile = {path: string; old_path: string; status: ChangeStatus; added: number; removed: number; binary: boolean; sig: string}
export type Changes = {is_repo: boolean; files: ChangedFile[]}
// kind ' ' context, '+' added, '-' removed; 0 = no line on that side.
export type DiffLine = {kind: ' ' | '+' | '-'; text: string; old_no: number; new_no: number}
export type Hunk = {old_start: number; old_lines: number; new_start: number; new_lines: number; lines: DiffLine[]}
// file_lines: the whole working-tree file (empty if deleted); too_large: no hunks and no file_lines.
export type FileDiff = {path: string; binary: boolean; too_large: boolean; hunks: Hunk[]; file_lines: string[]}

// One row of the orchestrator chat (Go: ChatItem in app/sessions.go). id is the underlying agent_events id,
// so it is unique and ordered within a session.
export type ChatItem =
  | {id: number; kind: 'user'; text: string; attachments?: AttachmentInfo[]; at: string}
  | {id: number; kind: 'assistant'; text: string; at: string}
  | {id: number; kind: 'tool'; name: string; summary: string; at: string}
  | {id: number; kind: 'escalation'; escalation: Escalation; at: string}
  | {id: number; kind: 'notice'; text: string; at: string} // a context compaction, or Fragile waking the orchestrator (one event per line)
