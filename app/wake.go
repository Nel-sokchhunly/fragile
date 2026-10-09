package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// Waking the orchestrator (interactive sessions only). Instead of looping on
// wait_for_notes, the orchestrator ends its turn; when a sub-agent posts a done,
// blocker or question note or exits, Fragile sends it one batched message.
// Events are debounced while it is idle, held while it is mid-turn (turnEnded
// flushes them) and dropped if it is not running (they are on the board anyway).
//
// Sub-agents (interactive Claude ones, fed over stdin) wait the same way. When a
// sub-agent's turn ends with its done note posted, its stdin is closed and it
// exits; without one it is idle (the orchestrator is told, so neither waits on
// the other unknowingly) and is woken, debounced, by any note someone else posts.
// Notes posted while it is mid-turn are held until its turn ends.

const (
	wakePrefix  = "[Fragile] Board update (automatic, not from the user):"
	wakeSuffix  = "Read the board for details."
	wakeLineMax = 200  // runes of a note's first line
	wakeMsgMax  = 4000 // bytes of all event lines together

	wakeDebounce = 2 * time.Second // batches a burst of events into one message
)

// wakeQueues holds each session's pending wake lines; the zero value is ready.
// Lock order: mu before App.mu. Never held while delivering.
type wakeQueues struct {
	mu   sync.Mutex
	m    map[int64]*wakeQueue // session -> its orchestrator's queue
	subs map[int64]*wakeQueue // sub-agent id -> its queue, from its first queued note or turn end until it exits
}

type wakeQueue struct {
	lines     []string
	timer     *time.Timer
	sessionID int64 // sub-agent queues only
	idle      bool  // sub-agent queues only: its turn ended without a done note
}

// queueWake (from react) queues the event's wake lines, if it has any, and arms the debounces.
func (a *App) queueWake(ev notes.Event) {
	if !a.runner.Interactive {
		return // the one-shot CLI has no stdin; its agents keep the wait loop
	}
	if line := a.wakeLine(ev); line != "" {
		a.queueOrchestrator(ev.SessionID, line)
	}
	if n, ok := ev.Payload.(notes.Note); ok && ev.Event == notes.EventNotePosted {
		a.queueSubagents(ev.SessionID, n)
	}
}

// queueOrchestrator queues a line for the session's orchestrator and arms the debounce.
func (a *App) queueOrchestrator(sessionID int64, line string) {
	a.wq.mu.Lock()
	defer a.wq.mu.Unlock()
	if a.wq.m == nil {
		a.wq.m = map[int64]*wakeQueue{}
	}
	q := a.wq.m[sessionID]
	if q == nil {
		q = &wakeQueue{}
		a.wq.m[sessionID] = q
	}
	q.lines = append(q.lines, line)
	if q.timer == nil {
		q.timer = time.AfterFunc(wakeDebounce, func() { a.flushWake(sessionID) })
	}
}

// queueSubagents queues a note for every sub-agent of the session fed over
// stdin except its author, and arms their debounces.
func (a *App) queueSubagents(sessionID int64, n notes.Note) {
	subs, err := a.store.ListAgents(sessionID, "subagent")
	if err != nil {
		return
	}
	var ids []int64
	for _, s := range subs {
		if s.ID != n.AuthorID && s.Status == "running" && a.runner.Running(s.ID) {
			ids = append(ids, s.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	line := noteLine(n, a.authorName(sessionID, n.AuthorID))
	a.wq.mu.Lock()
	defer a.wq.mu.Unlock()
	for _, id := range ids {
		q := a.subQueue(sessionID, id)
		q.lines = append(q.lines, line)
		if q.timer == nil {
			q.timer = time.AfterFunc(wakeDebounce, func() { a.flushSub(id) })
		}
	}
}

// subQueue returns the sub-agent's queue, creating it busy; the caller holds wq.mu.
func (a *App) subQueue(sessionID, agentID int64) *wakeQueue {
	if a.wq.subs == nil {
		a.wq.subs = map[int64]*wakeQueue{}
	}
	q := a.wq.subs[agentID]
	if q == nil {
		q = &wakeQueue{sessionID: sessionID}
		a.wq.subs[agentID] = q
	}
	return q
}

// wakeLine is the message line for an event that wakes the orchestrator, "" for any other.
func (a *App) wakeLine(ev notes.Event) string {
	switch ev.Event {
	case notes.EventNotePosted:
		n, ok := ev.Payload.(notes.Note)
		if !ok || (n.Type != "done" && n.Type != "blocker" && n.Type != "question") {
			return "" // decision and heads_up notes do not wake
		}
		from := a.authorName(ev.SessionID, n.AuthorID)
		if from == "the orchestrator" {
			return ""
		}
		return noteLine(n, from)
	case notes.EventAgentStatusChanged:
		p, ok := ev.Payload.(map[string]any)
		if !ok || p["role"] != "subagent" {
			return ""
		}
		line := fmt.Sprintf("agent %d exited", ev.AgentID)
		switch p["status"] {
		case "exited":
		case "crashed", "stopped":
			line += fmt.Sprintf(" (%s)", p["status"])
		default:
			return ""
		}
		if p["missing_done_note"] == true {
			line += " without a done note"
		}
		return line
	}
	return ""
}

// authorName names a note's author in a wake line.
func (a *App) authorName(sessionID, authorID int64) string {
	if authorID == 0 {
		return "the user"
	}
	if ag, err := a.store.GetAgent(sessionID, authorID); err == nil && ag.Role == "orchestrator" {
		return "the orchestrator"
	}
	return fmt.Sprintf("agent %d", authorID)
}

// noteLine is a note's wake line: id, type, author and the first line of its content.
func noteLine(n notes.Note, from string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(n.Content), "\n")
	if rs := []rune(first); len(rs) > wakeLineMax {
		first = string(rs[:wakeLineMax]) + "..."
	}
	return fmt.Sprintf("#%d %s from %s: %s", n.ID, n.Type, from, first)
}

// wakeMessage joins the lines into one message, capped at wakeMsgMax.
func wakeMessage(lines []string) string {
	var b strings.Builder
	b.WriteString(wakePrefix + "\n")
	for i, l := range lines {
		if b.Len()+len(l) > wakeMsgMax {
			fmt.Fprintf(&b, "- ... and %d more\n", len(lines)-i)
			break
		}
		b.WriteString("- " + l + "\n")
	}
	return b.String() + wakeSuffix
}

// flushWake delivers the session's pending lines as one message if the
// orchestrator is idle; while it is mid-turn they stay queued for turnEnded.
func (a *App) flushWake(sessionID int64) {
	a.wq.mu.Lock()
	q := a.wq.m[sessionID]
	if q == nil {
		a.wq.mu.Unlock()
		return
	}
	if q.timer != nil {
		q.timer.Stop()
		q.timer = nil
	}
	a.mu.Lock()
	busy := a.busy[sessionID]
	a.mu.Unlock()
	if busy {
		a.wq.mu.Unlock()
		return
	}
	lines := q.lines
	delete(a.wq.m, sessionID)
	a.wq.mu.Unlock()
	if len(lines) == 0 {
		return
	}
	orch, err := a.liveOrchestrator(sessionID)
	if err != nil {
		return // not running: the events are on the board for when it resumes
	}
	if err := a.deliver(orch, wakeMessage(lines), nil, true); err != nil {
		log.Printf("session %d: waking the orchestrator: %v", sessionID, err)
	}
}

// dropWakes forgets the orchestrator's pending lines (it ended, or the session
// is gone) and, with all, its sub-agents' too (the session is gone).
func (a *App) dropWakes(sessionID int64, all bool) {
	a.wq.mu.Lock()
	defer a.wq.mu.Unlock()
	if q := a.wq.m[sessionID]; q != nil && q.timer != nil {
		q.timer.Stop()
	}
	delete(a.wq.m, sessionID)
	for id, q := range a.wq.subs {
		if all && q.sessionID == sessionID {
			if q.timer != nil {
				q.timer.Stop()
			}
			delete(a.wq.subs, id)
		}
	}
}

// dropSubWakes forgets a sub-agent's queue (it exited, or is about to).
func (a *App) dropSubWakes(agentID int64) {
	a.wq.mu.Lock()
	defer a.wq.mu.Unlock()
	if q := a.wq.subs[agentID]; q != nil && q.timer != nil {
		q.timer.Stop()
	}
	delete(a.wq.subs, agentID)
}

// flushSub delivers the sub-agent's pending lines as one message if it is
// idle; while it is mid-turn they stay queued for subTurnEnded.
func (a *App) flushSub(agentID int64) {
	a.wq.mu.Lock()
	q := a.wq.subs[agentID]
	if q == nil {
		a.wq.mu.Unlock()
		return
	}
	if q.timer != nil {
		q.timer.Stop()
		q.timer = nil
	}
	if !q.idle || len(q.lines) == 0 {
		a.wq.mu.Unlock()
		return
	}
	lines, sessionID := q.lines, q.sessionID
	q.lines, q.idle = nil, false // busy from the message on
	a.wq.mu.Unlock()
	// Stored like the orchestrator's messages: the row first, published once written.
	msg := wakeMessage(lines)
	b, _ := json.Marshal(userMessage{Text: msg})
	ev, err := a.store.AppendAgentEvent(sessionID, agentID, evUserMessage, string(b))
	if err != nil {
		log.Printf("agent %d: storing wake: %v", agentID, err)
		return
	}
	if err := a.runner.SendUser(agentID, msg); err != nil {
		a.store.DeleteAgentEvent(sessionID, agentID, ev.ID)
		return // it exited; its status change drops the queue
	}
	a.publish(notes.Agent{ID: agentID, SessionID: sessionID, Role: "subagent"}, ev)
}

// subTurnEnded runs when a sub-agent's turn ends, outside any lock. With its
// done note posted its stdin is closed, so it exits; otherwise it is idle:
// pending notes go out now, else the orchestrator is told it waits.
func (a *App) subTurnEnded(ag notes.Agent) {
	if !a.runner.Running(ag.ID) {
		return // one-shot or Codex: no stdin, it exits on its own
	}
	board, err := a.store.SessionBoard(ag.SessionID)
	if err != nil {
		return
	}
	done, err := a.store.ListNotes(ag.SessionID, board, notes.NoteFilter{Type: "done", AuthorID: ag.ID})
	if err != nil {
		return
	}
	if len(done) > 0 {
		a.dropSubWakes(ag.ID)
		a.runner.CloseStdin(ag.ID)
		return
	}
	a.wq.mu.Lock()
	q := a.subQueue(ag.SessionID, ag.ID)
	q.idle = true
	pending := len(q.lines) > 0
	a.wq.mu.Unlock()
	if pending {
		a.flushSub(ag.ID)
		return
	}
	a.queueOrchestrator(ag.SessionID, fmt.Sprintf("agent %d idle, waiting (no done note)", ag.ID))
}

// turnEnded runs when an orchestrator turn ends (busy true -> false), outside any
// lock: an auto-compact goes first (its own turn end flushes), else pending wakes go out.
func (a *App) turnEnded(sessionID int64) {
	if a.maybeAutoCompact(sessionID) {
		return
	}
	a.flushWake(sessionID)
}
