package main

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

// compactBoundary is the part of Claude Code's system/compact_boundary line the app uses.
type compactBoundary struct {
	Subtype  string `json:"subtype"`
	Metadata struct {
		Trigger    string `json:"trigger"` // manual | auto
		PreTokens  int    `json:"pre_tokens"`
		PostTokens int    `json:"post_tokens"`
	} `json:"compact_metadata"`
}

// CompactSession asks the orchestrator to compact its context (Claude Code's
// /compact). It errors if the orchestrator is not running or is mid-turn. The
// command is not shown as a chat message; the compact_boundary line it causes
// is (see chatItem), and its result line ends the turn as usual.
func (a *App) CompactSession(sessionID int64) error {
	orch, err := a.liveOrchestrator(sessionID)
	if err != nil {
		return err
	}
	a.mu.Lock()
	busy := a.busy[sessionID]
	a.mu.Unlock()
	if busy {
		return errors.New("the orchestrator is working; wait or interrupt it first")
	}
	return a.deliver(orch, "/compact", nil, false)
}

// compactNotice is the chat text for a compaction: "Context compacted (manual): 25.4k → 4.7k tokens".
func compactNotice(c compactBoundary) string {
	trigger := c.Metadata.Trigger
	if trigger == "" {
		trigger = "auto"
	}
	return fmt.Sprintf("Context compacted (%s): %s → %s tokens", trigger, tokenCount(c.Metadata.PreTokens), tokenCount(c.Metadata.PostTokens))
}

// tokenCount formats a token count like the UI header: 28726 -> "28.7k", 1000000 -> "1M".
func tokenCount(n int) string {
	f := func(v float64, unit string) string {
		if v >= 100 || v == math.Trunc(v) {
			return strconv.Itoa(int(math.Round(v))) + unit
		}
		return strconv.FormatFloat(v, 'f', 1, 64) + unit
	}
	switch {
	case n >= 1e6:
		return f(float64(n)/1e6, "M")
	case n >= 1e3:
		return f(float64(n)/1e3, "k")
	}
	return strconv.Itoa(n)
}
