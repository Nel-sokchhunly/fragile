package main

import "log"

// eventSessionCompacting tells the UI a compaction started without its button
// (auto-compact), so it shows the "compacting" state. Payload nil; the UI clears
// it on the orchestrator's next result, as for a manual compact.
const eventSessionCompacting = "session_compacting"

// maybeAutoCompact runs when an orchestrator turn ends. If the auto-compact
// threshold is set and the orchestrator's context has reached it, it starts a
// compaction (CompactSession) and returns true; the compact's own result ends a
// turn too, and that check is skipped so it can never compact twice in a row.
func (a *App) maybeAutoCompact(sessionID int64) bool {
	a.prefs.mu.Lock()
	if a.prefs.lastAuto[sessionID] { // this turn is the auto-compact's own result
		delete(a.prefs.lastAuto, sessionID)
		a.prefs.mu.Unlock()
		return false
	}
	a.prefs.mu.Unlock()

	s, err := a.GetSettings()
	if err != nil {
		log.Printf("session %d: auto-compact: reading settings: %v", sessionID, err)
		return false
	}
	if s.AutoCompactTokens == 0 {
		return false
	}
	orch, err := a.liveOrchestrator(sessionID)
	if err != nil || orch.ContextUsed < s.AutoCompactTokens {
		return false
	}

	// Mark before starting: the compact's result can arrive before CompactSession returns.
	a.prefs.mu.Lock()
	if a.prefs.lastAuto == nil {
		a.prefs.lastAuto = map[int64]bool{}
	}
	a.prefs.lastAuto[sessionID] = true
	a.prefs.mu.Unlock()
	if err := a.CompactSession(sessionID); err != nil { // e.g. the orchestrator is mid-turn again
		a.prefs.mu.Lock()
		delete(a.prefs.lastAuto, sessionID)
		a.prefs.mu.Unlock()
		log.Printf("session %d: auto-compact not started: %v", sessionID, err)
		return false
	}
	log.Printf("session %d: auto-compacting (context %d >= %d tokens)", sessionID, orch.ContextUsed, s.AutoCompactTokens)
	a.pushEvent(eventSessionCompacting, sessionID, orch.ID, nil)
	return true
}
