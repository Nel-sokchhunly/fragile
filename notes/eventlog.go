package notes

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

// Observation log event types.
const (
	EventNotePosted         = "note_posted"
	EventNoteUpdated        = "note_updated"
	EventAgentSpawned       = "agent_spawned"
	EventAgentStatusChanged = "agent_status_changed"
	EventEscalation         = "escalation"
	// For the session manager; emitted by whoever changes the session, via Write.
	EventSessionCreated       = "session_created"
	EventSessionStatusChanged = "session_status_changed"
	EventSessionDeleted       = "session_deleted" // no payload; the session and its rows are gone
)

// Event is one observation: a line of the log, and what OnEvent receives.
type Event struct {
	Time      string `json:"time"`
	Event     string `json:"event"`
	SessionID int64  `json:"session_id"`
	AgentID   int64  `json:"agent_id,omitempty"`
	Payload   any    `json:"payload,omitempty"`
}

// EventLog appends one JSON object per line to a file the user can `tail -f`.
// Writes go straight to the file (no buffering), so each event is visible at once.
// One log serves every session; each event carries its session_id.
type EventLog struct {
	// OnEvent, if set before the log is used, is called for every event after it
	// is written, in log order and under the log's lock: it must return quickly
	// (hand off to a channel) and must not call Write. Payload is the in-memory value.
	OnEvent func(Event)

	mu sync.Mutex
	f  *os.File // nil when the log has no file

	wmu     sync.Mutex // guards waiters
	waiters map[int64]chan struct{}
}

// Changed returns a channel that is closed on the next event of the session.
// Waiters (wait_for_notes) take it BEFORE checking the store, then block on it,
// so a change between the check and the block is never missed. Events are
// written after the store change they report, so a wake-up always sees it.
func (l *EventLog) Changed(sessionID int64) <-chan struct{} {
	l.wmu.Lock()
	defer l.wmu.Unlock()
	ch := l.waiters[sessionID]
	if ch == nil {
		if l.waiters == nil {
			l.waiters = map[int64]chan struct{}{}
		}
		ch = make(chan struct{})
		l.waiters[sessionID] = ch
	}
	return ch
}

func (l *EventLog) wake(sessionID int64) {
	l.wmu.Lock()
	defer l.wmu.Unlock()
	if ch := l.waiters[sessionID]; ch != nil {
		close(ch)
		delete(l.waiters, sessionID)
	}
}

// OpenEventLog opens the log file at path. An empty path gives a log with no
// file, only OnEvent (for an embedder that does not want a file).
func OpenEventLog(path string) (*EventLog, error) {
	if path == "" {
		return &EventLog{}, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &EventLog{f: f}, nil
}

// Write appends an event. agentID 0 means the event has no agent. A failure is
// also reported on stderr, so callers that cannot act on it may ignore the error.
func (l *EventLog) Write(event string, sessionID, agentID int64, payload any) error {
	err := l.write(Event{time.Now().UTC().Format(time.RFC3339Nano), event, sessionID, agentID, payload})
	if err != nil {
		log.Printf("event log: writing %s: %v", event, err)
	}
	return err
}

func (l *EventLog) write(ev Event) error {
	defer l.wake(ev.SessionID) // even if marshalling or the file write fails: the store already changed
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_, err = l.f.Write(append(line, '\n'))
	}
	if l.OnEvent != nil {
		l.OnEvent(ev) // even if the file write failed: the UI should still see it
	}
	return err
}

func (l *EventLog) Close() error {
	if l.f == nil {
		return nil
	}
	return l.f.Close()
}
