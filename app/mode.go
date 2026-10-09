package main

import (
	"errors"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// switchMessage is the first message the agent gets after a normal session is
// switched to orchestra mode.
const switchMessage = "The user switched this session to orchestra mode. You are now the orchestrator: " +
	"you have the Fragile tools (spawn_subagent, the board, escalation) and the orchestrator instructions " +
	"in your system prompt. Keep the conversation so far in mind; delegate work to sub-agents from now on. " +
	"Briefly confirm, then continue with whatever the user last asked."

// SetSessionMode changes a session between ModeNormal and ModeOrchestra.
// Normal to orchestra keeps the conversation: an idle agent is stopped and
// relaunched resuming the same provider conversation (Claude --resume, Codex
// thread/resume, agy --conversation) with the Fragile MCP tools and the
// orchestrator prompt, and is told it is now the orchestrator. Orchestra to
// normal is only possible before the session's first message.
func (a *App) SetSessionMode(sessionID int64, mode string) (notes.Session, error) {
	if err := notes.CheckMode(mode); err != nil {
		return notes.Session{}, err
	}
	se, err := a.store.GetSession(sessionID)
	if err != nil {
		return notes.Session{}, err
	}
	if se.Mode == mode {
		return se, nil
	}
	a.startMu.Lock()
	orch, err := a.switchMode(sessionID, mode)
	a.startMu.Unlock()
	if err != nil {
		return notes.Session{}, err
	}
	if orch.ID != 0 {
		if err := a.deliver(orch, switchMessage, nil, false); err != nil {
			return notes.Session{}, err
		}
	}
	return a.store.GetSession(sessionID)
}

// switchMode stores the mode and, when a live agent must change with it,
// returns the relaunched one (zero otherwise); the caller holds a.startMu.
func (a *App) switchMode(sessionID int64, mode string) (notes.Agent, error) {
	orchs, err := a.store.ListAgents(sessionID, "orchestrator")
	if err != nil {
		return notes.Agent{}, err
	}
	if a.neverStarted(sessionID, orchs) {
		return notes.Agent{}, a.store.SetSessionMode(sessionID, mode)
	}
	if mode == notes.ModeNormal {
		return notes.Agent{}, errors.New("a session that has started cannot go back to normal mode")
	}
	last := orchs[len(orchs)-1]
	if !a.runner.Running(last.ID) { // stopped: the mode applies when the session is resumed
		return notes.Agent{}, a.store.SetSessionMode(sessionID, mode)
	}
	a.mu.Lock()
	busy := a.busy[sessionID]
	a.mu.Unlock()
	if busy {
		return notes.Agent{}, errors.New("the agent is replying; switch when it has finished")
	}
	providerID, err := a.providerSessionID(sessionID, orchs)
	if err != nil {
		return notes.Agent{}, err
	}
	if err := a.runner.StopAgent(last.ID, true); err != nil {
		return notes.Agent{}, err
	}
	if err := a.store.SetSessionMode(sessionID, mode); err != nil {
		return notes.Agent{}, err
	}
	a.setBusy(sessionID, true) // before the process exists, so the session never flickers to done
	orch, err := a.runner.ResumeOrchestrator(sessionID, providerID)
	if err != nil {
		a.setBusy(sessionID, false)
		a.store.SetSessionMode(sessionID, notes.ModeNormal) // a later ResumeSession brings the agent back as it was
		return notes.Agent{}, err
	}
	return orch, nil
}
