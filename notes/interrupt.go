package notes

import (
	"encoding/json"
	"errors"
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
	agy := false
	for _, p := range r.running {
		if p.agentID == agentID {
			in, agy = p.stdin, p.agy != nil
		}
	}
	r.mu.Unlock()
	if in == nil {
		return ErrNotRunning
	}
	if agy { // agy's stream-json input has no interrupt message
		return errors.New("interrupt is not supported for Antigravity; wait for the turn to finish or stop the session")
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
