package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// Per-sub-agent actions (decision #65). Pause interrupts a busy sub-agent's
// turn and holds its automatic wakes; Resume continues a paused or idle one, or
// relaunches one that stopped without a done note on the same agent id; Finish
// ends one. The orchestrator has the session controls instead.

const (
	eventAgentActivity = "agent_activity" // payload agentActivityEvent: a running sub-agent's busy or paused changed

	resumedMessage          = fragilePrefix + "Resumed by the user. Continue your task."
	resumedAfterStopMessage = fragilePrefix + "Resumed by the user after it stopped. Continue your task; read_notes for what changed."
)

// AgentActivity is a running sub-agent's live state: Busy while mid-turn,
// Paused from PauseAgent until ResumeAgent (it gets no automatic wakes meanwhile).
type AgentActivity struct {
	Busy   bool `json:"busy"`
	Paused bool `json:"paused"`
	// Live: the sub-agent takes messages on stdin (see liveSubagent). Only the
	// snapshot sets it; it cannot change while the agent runs.
	Live bool `json:"live,omitempty"`
}

type agentActivityEvent struct {
	AgentID int64 `json:"agent_id"`
	AgentActivity
}

// setActivity sets a sub-agent's idle and paused flags and emits agent_activity
// if either changed; the caller holds wq.mu.
func (a *App) setActivity(agentID int64, q *wakeQueue, idle, paused bool) {
	if q.idle == idle && q.paused == paused {
		return
	}
	q.idle, q.paused = idle, paused
	a.pushEvent(eventAgentActivity, q.sessionID, agentID, agentActivityEvent{agentID, AgentActivity{Busy: !idle, Paused: paused}})
}

// agentActivity is the snapshot's activity of the running sub-agents among
// agents. One without a queue has not ended a turn yet: busy.
func (a *App) agentActivity(agents []notes.Agent) map[int64]AgentActivity {
	out := map[int64]AgentActivity{}
	a.wq.mu.Lock()
	defer a.wq.mu.Unlock()
	for _, ag := range agents {
		if ag.Role == "orchestrator" || ag.Status != "running" {
			continue
		}
		act := AgentActivity{Busy: true}
		if q := a.wq.subs[ag.ID]; q != nil {
			act = AgentActivity{Busy: !q.idle, Paused: q.paused}
		}
		act.Live = a.runner.Running(ag.ID)
		out[ag.ID] = act
	}
	return out
}

// subagent looks up a sub-agent by id alone.
func (a *App) subagent(agentID int64) (notes.Agent, error) {
	ag, err := a.store.FindAgent(agentID)
	switch {
	case errors.Is(err, notes.ErrNotFound):
		return ag, fmt.Errorf("agent %d not found", agentID)
	case err != nil:
		return ag, err
	case ag.Role == "orchestrator":
		return ag, fmt.Errorf("agent %d is the orchestrator; use the session controls", agentID)
	}
	return ag, nil
}

// liveSubagent checks that ag runs and takes messages on stdin.
func (a *App) liveSubagent(ag notes.Agent) error {
	if ag.Status != "running" {
		return fmt.Errorf("agent %d is not running", ag.ID)
	}
	if !a.runner.Running(ag.ID) {
		return fmt.Errorf("agent %d takes no messages; only interactive Claude sub-agents can be paused or resumed while running", ag.ID)
	}
	return nil
}

// PauseAgent interrupts a busy sub-agent's turn; its process and conversation
// stay. Until ResumeAgent it gets no automatic wakes (they stay queued).
func (a *App) PauseAgent(agentID int64) error {
	ag, err := a.subagent(agentID)
	if err != nil {
		return err
	}
	if err := a.liveSubagent(ag); err != nil {
		return err
	}
	a.wq.mu.Lock()
	q := a.subQueue(ag.SessionID, ag.ID)
	switch {
	case q.paused:
		err = fmt.Errorf("agent %d is already paused", ag.ID)
	case q.idle:
		err = fmt.Errorf("agent %d is idle; there is nothing to pause", ag.ID)
	default:
		a.setActivity(ag.ID, q, false, true) // the turn end (subTurnEnded) makes it idle
	}
	a.wq.mu.Unlock()
	if err != nil {
		return err
	}
	if err := a.runner.Interrupt(ag.ID); err != nil {
		a.wq.mu.Lock()
		if q := a.wq.subs[ag.ID]; q != nil {
			a.setActivity(ag.ID, q, q.idle, false)
		}
		a.wq.mu.Unlock()
		return err
	}
	return nil
}

// ResumeAgent continues a paused or idle running sub-agent with a message that
// carries its queued wakes, or relaunches one that exited, stopped or crashed
// without a done note, resuming its Claude Code conversation on the same id.
func (a *App) ResumeAgent(agentID int64) error {
	ag, err := a.subagent(agentID)
	if err != nil {
		return err
	}
	if ag.Status != "running" {
		return a.relaunchAgent(ag)
	}
	if err := a.liveSubagent(ag); err != nil {
		return err
	}
	a.wq.mu.Lock()
	q := a.subQueue(ag.SessionID, ag.ID)
	switch {
	case !q.idle && q.paused:
		err = fmt.Errorf("agent %d is still ending its interrupted turn; try again in a moment", ag.ID)
	case !q.idle:
		err = fmt.Errorf("agent %d is working; there is nothing to resume", ag.ID)
	}
	if err != nil {
		a.wq.mu.Unlock()
		return err
	}
	if q.timer != nil {
		q.timer.Stop()
		q.timer = nil
	}
	lines := q.lines
	q.lines = nil
	a.setActivity(ag.ID, q, false, false)
	a.wq.mu.Unlock()
	msg := resumedMessage
	if len(lines) > 0 {
		msg += "\n\n" + wakeMessage(lines)
	}
	return a.sendSub(ag.SessionID, ag.ID, msg)
}

// MessageAgent sends the user's message to a running interactive sub-agent and
// tells the orchestrator with a heads_up note. It goes out at once: Claude Code
// queues stdin messages that arrive mid-turn for the next turn boundary. An idle
// or paused agent is resumed by it and gets its queued wakes with it.
func (a *App) MessageAgent(agentID int64, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("message must not be empty")
	}
	ag, err := a.subagent(agentID)
	if err != nil {
		return err
	}
	if err := a.liveSubagent(ag); err != nil {
		return err
	}
	a.wq.mu.Lock()
	q := a.subQueue(ag.SessionID, ag.ID)
	msg := text
	if q.idle || q.paused {
		if q.timer != nil {
			q.timer.Stop()
			q.timer = nil
		}
		if len(q.lines) > 0 {
			msg += "\n\n" + wakeMessage(q.lines)
		}
		q.lines = nil
		a.setActivity(ag.ID, q, false, false)
	}
	a.wq.mu.Unlock()
	if err := a.sendSub(ag.SessionID, ag.ID, msg); err != nil {
		return err
	}
	first := []rune(text)
	preview := string(first)
	if len(first) > wakeLineMax {
		preview = string(first[:wakeLineMax]) + "..."
	}
	_, err = a.AddNote(ag.SessionID, "heads_up", fmt.Sprintf("User messaged sub-agent #%d (%s): %s", ag.ID, a.agentTitle(ag), preview))
	return err
}

// agentTitle names a sub-agent for a note: its task title, else its role.
func (a *App) agentTitle(ag notes.Agent) string {
	if ag.TaskID != 0 {
		if t, err := a.store.GetTask(ag.SessionID, ag.TaskID); err == nil && t.Title != "" {
			return t.Title
		}
	}
	return "sub-agent"
}

// relaunchAgent resumes a sub-agent that is no longer running.
func (a *App) relaunchAgent(ag notes.Agent) error {
	board, err := a.store.SessionBoard(ag.SessionID)
	if err != nil {
		return err
	}
	done, err := a.store.ListNotes(ag.SessionID, board, notes.NoteFilter{Type: "done", AuthorID: ag.ID})
	if err != nil {
		return err
	}
	if len(done) > 0 {
		return fmt.Errorf("agent %d already posted its done note; there is nothing to resume", ag.ID)
	}
	se, err := a.store.GetSession(ag.SessionID)
	if err != nil {
		return err
	}
	prov := ag.Provider
	if prov == "" {
		prov = se.Provider
	}
	if prov != notes.ProviderClaude {
		return fmt.Errorf("agent %d cannot be resumed: only Claude sub-agents can be resumed after they stop", ag.ID)
	}
	providerID, err := a.providerSessionID(ag.SessionID, []notes.Agent{ag})
	if err != nil {
		return fmt.Errorf("agent %d has no recorded Claude Code conversation, so it cannot be resumed", ag.ID)
	}
	if _, err := a.runner.ResumeSubagent(ag.SessionID, ag.ID, providerID, resumedAfterStopMessage); err != nil {
		return err
	}
	b, _ := json.Marshal(userMessage{Text: resumedAfterStopMessage})
	_, err = a.record(ag, evUserMessage, string(b)) // shown in its output; the runner sent it
	return err
}

// RestartAgent relaunches a sub-agent's task as a new sub-agent: a fresh
// process and conversation (nothing resumed) with the old one's task text,
// title, provider, model and private scopes, under the same orchestrator. A
// running agent is stopped first. The new agent gets its own task record, which
// repeats the old one's text; the heads_up note links the two agents.
func (a *App) RestartAgent(agentID int64) (int64, error) {
	ag, err := a.subagent(agentID)
	if err != nil {
		return 0, err
	}
	t, err := a.store.GetTask(ag.SessionID, ag.TaskID)
	if err != nil {
		return 0, fmt.Errorf("agent %d has no task to restart: %w", ag.ID, err)
	}
	if ag.Status == "running" {
		if err := a.FinishAgent(ag.ID); err != nil {
			return 0, err
		}
	}
	scopes, err := a.store.AgentScopes(ag.SessionID, ag.ID)
	if err != nil {
		return 0, err
	}
	var private []string
	for _, sc := range scopes {
		if sc.Name != "session" {
			private = append(private, sc.Name)
		}
	}
	n, err := a.runner.SpawnSubagentScoped(ag.SessionID, ag.ParentID, t.Title, t.Description, ag.Model, ag.Provider, private)
	if err != nil {
		return 0, err
	}
	_, err = a.AddNote(ag.SessionID, "heads_up", fmt.Sprintf("User restarted sub-agent #%d as #%d (%s)", ag.ID, n.ID, t.Title))
	return n.ID, err
}

// FinishAgent ends a running sub-agent: an idle one by closing its stdin, a
// busy one by signals. It is recorded as stopped, which wakes the orchestrator.
// It returns once the process is gone.
func (a *App) FinishAgent(agentID int64) error {
	ag, err := a.subagent(agentID)
	if err != nil {
		return err
	}
	if ag.Status != "running" {
		return fmt.Errorf("agent %d is not running", ag.ID)
	}
	a.wq.mu.Lock()
	q := a.wq.subs[ag.ID]
	idle := q != nil && q.idle
	a.wq.mu.Unlock()
	a.dropSubWakes(ag.ID) // nothing more to wake it with
	if err := a.runner.StopAgent(ag.ID, idle); errors.Is(err, notes.ErrNotRunning) {
		return fmt.Errorf("agent %d is not running", ag.ID)
	} else {
		return err
	}
}
