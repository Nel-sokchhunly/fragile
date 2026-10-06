import {EventsOn} from '../../wailsjs/runtime/runtime'
import type {Agent, AgentEvent, AgentStatus, AgentRole, ChatItem, Escalation, Note, Session, SessionStatus, Task, TaskStatus} from './types'

// Go -> Wails events -> Zustand store -> components. Components never poll.
// Event names and payloads live here; keep in sync with the emitters in app/*.go
// (names: notes/eventlog.go and the event* constants in app/app.go).

// Every event arrives wrapped in this envelope (Go: notes.Event). `agent_id` is absent when the
// event has no agent; `session_id` is always set, so the store routes by it.
export type Envelope<P> = {
  time: string // RFC 3339
  event: string
  session_id: number
  agent_id?: number
  payload: P
}

export type EventMap = {
  // Session lifecycle. session_created carries the full row; status changes carry only the new status.
  session_created: Envelope<Session>
  session_status_changed: Envelope<{status: SessionStatus}>

  // Agents and tasks. agent_spawned / agent_status_changed are the raw observations; agent_updated and
  // task_updated follow each of them with the full row, so the store can simply upsert by id.
  agent_spawned: Envelope<{role: AgentRole; parent_id?: number; task_id?: number; pid: number; log_path: string; task: string}>
  agent_status_changed: Envelope<{
    role: AgentRole
    status: AgentStatus
    exit_code?: number
    task_id?: number
    task_status?: TaskStatus
    missing_done_note?: boolean // a sub-agent exited cleanly without posting a `done` note
    error?: string
    reason?: string // "app restarted" for agents recovered at startup
  }>
  agent_updated: Envelope<Agent>
  task_updated: Envelope<Task>

  // New row of an agent's output (assistant_text {text}, tool_use {id,name,input}, tool_result {tool_use_id,content,is_error},
  // user_message {text}, escalation {escalation_id}, plus raw system / result). Append to agentEvents[agent_id].
  // May arrive just before that agent's agent_spawned.
  agent_event: Envelope<AgentEvent>
  // Upsert by `id` into the session's orchestrator chat (an answered escalation re-arrives with the same id).
  chat_item: Envelope<ChatItem>

  // Notes board. author_agent_id 0 = the user.
  note_posted: Envelope<Note>
  note_updated: Envelope<Note>

  // An escalation was raised (status open) or answered. The chat shows it via chat_item; session status
  // follows via session_status_changed.
  escalation: Envelope<Escalation>
}

// Subscribe to one event; returns its unsubscribe function.
export function on<K extends keyof EventMap>(name: K, handler: (e: EventMap[K]) => void): () => void {
  return EventsOn(name, handler)
}

// Call once at startup. Add one `on(...)` per event, each writing into a store.
export function subscribeEvents() {
  const offs: (() => void)[] = [] // e.g. offs.push(on('session_status_changed', (e) => useAppStore.getState()...))
  return () => offs.forEach((off) => off())
}
