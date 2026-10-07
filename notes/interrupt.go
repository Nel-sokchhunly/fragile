package notes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
)

// interruptSeq numbers interrupt requests; Claude Code echoes the id in its control_response.
var interruptSeq atomic.Int64

// Interrupt asks the agent to end its current turn, the way the Agent SDK does:
// a stream-json control_request with subtype "interrupt" on its stdin. The
// process and conversation stay alive; Claude Code answers with a
// control_response and ends the turn with a result line. It returns
// ErrNotRunning if the agent has no live process taking messages.
func (r *Runner) Interrupt(agentID int64) error {
	r.mu.Lock()
	var in *stdinPipe
	for _, p := range r.running {
		if p.agentID == agentID {
			in = p.stdin
		}
	}
	r.mu.Unlock()
	if in == nil {
		return ErrNotRunning
	}
	line, err := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": "fragile-interrupt-" + strconv.FormatInt(interruptSeq.Add(1), 10),
		"request":    map[string]string{"subtype": "interrupt"},
	})
	if err != nil {
		return err
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if _, err := in.w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("%w: %v", ErrNotRunning, err)
	}
	return nil
}
