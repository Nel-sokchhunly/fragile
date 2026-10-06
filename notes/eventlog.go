package notes

import (
	"encoding/json"
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
)

// EventLog appends one JSON object per line to a file the user can `tail -f`.
// Writes go straight to the file (no buffering), so each event is visible at once.
type EventLog struct {
	mu sync.Mutex
	f  *os.File
}

func OpenEventLog(path string) (*EventLog, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &EventLog{f: f}, nil
}

// Write appends an event. agentID 0 means the event has no agent.
func (l *EventLog) Write(event string, agentID int64, payload any) error {
	line, err := json.Marshal(struct {
		Time    string `json:"time"`
		Event   string `json:"event"`
		AgentID int64  `json:"agent_id,omitempty"`
		Payload any    `json:"payload,omitempty"`
	}{time.Now().UTC().Format(time.RFC3339Nano), event, agentID, payload})
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.f.Write(append(line, '\n'))
	return err
}

func (l *EventLog) Close() error { return l.f.Close() }
