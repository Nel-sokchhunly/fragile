// TS mirrors of the Go structs in notes/store.go (JSON tags). Timestamps are RFC 3339 strings.
// Go `omitempty` ids/strings are optional here; 0 / absent means "none".

export type SessionStatus = 'working' | 'done' | 'needs_you'
export type AgentRole = 'orchestrator' | 'subagent'
export type AgentStatus = 'running' | 'exited' | 'crashed'
export type TaskStatus = 'planned' | 'working' | 'blocked' | 'review' | 'done'
export type NoteType = 'decision' | 'blocker' | 'heads_up' | 'done' | 'question'
export type NoteStatus = 'open' | 'resolved'
export type EscalationStatus = 'open' | 'answered'

export type Session = {
  id: number
  title: string
  status: SessionStatus
  work_dir?: string // directory its agents run in; absent/'' for sessions without one
  created_at: string
}

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

// One row of the orchestrator chat (Go: ChatItem in app/sessions.go). id is the underlying agent_events id,
// so it is unique and ordered within a session.
export type ChatItem =
  | {id: number; kind: 'user'; text: string; at: string}
  | {id: number; kind: 'assistant'; text: string; at: string}
  | {id: number; kind: 'tool'; name: string; summary: string; at: string}
  | {id: number; kind: 'escalation'; escalation: Escalation; at: string}
