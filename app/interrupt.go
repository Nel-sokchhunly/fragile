package main

// InterruptSession cancels the orchestrator's current turn; the process, its
// conversation and the sub-agents keep running. It errors if the orchestrator
// is not running and does nothing if it is idle. The busy state clears when
// Claude Code ends the turn with its result line (see onLine).
func (a *App) InterruptSession(sessionID int64) error {
	orch, err := a.liveOrchestrator(sessionID)
	if err != nil {
		return err
	}
	a.mu.Lock()
	busy := a.busy[sessionID]
	a.mu.Unlock()
	if !busy {
		return nil
	}
	return a.runner.Interrupt(orch.ID)
}
