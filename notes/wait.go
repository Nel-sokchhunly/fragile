package notes

import (
	"context"
	"errors"
	"time"
)

const (
	defaultWait = 60 * time.Second
	maxWait     = 120 * time.Second
)

type waitIn struct {
	SinceID           int64  `json:"since_id" jsonschema:"return notes with an id greater than this (use the highest id you have seen; 0 for all)"`
	TimeoutS          int    `json:"timeout_s,omitempty" jsonschema:"seconds to block at most; default 60, max 120"`
	Type              string `json:"type,omitempty" jsonschema:"only wake for notes of this type, e.g. done"`
	Scope             string `json:"scope,omitempty" jsonschema:"only wake for notes in this scope; default every scope you can access"`
	FinishedSubagents *int   `json:"finished_subagents,omitempty" jsonschema:"orchestrator only: also wake when more sub-agents than this have exited or crashed (pass the finished_subagents value of your previous call, 0 at first)"`
}

type waitOut struct {
	Notes             []Note `json:"notes"`
	FinishedSubagents *int   `json:"finished_subagents,omitempty"`
	RunningSubagents  *int   `json:"running_subagents,omitempty"`
}

// waitForNotes blocks until a matching note exists, an orchestrator's
// sub-agent has finished, or the timeout passes. Waking comes from
// EventLog.Changed (no polling); a timeout is an empty result, not an error.
func (s *Server) waitForNotes(ctx context.Context, a Agent, in waitIn) (any, error) {
	if in.Type != "" {
		if err := checkEnum("type", in.Type, noteTypes); err != nil {
			return nil, err
		}
	}
	if in.FinishedSubagents != nil && a.Role != roleOrchestrator {
		return nil, errors.New("finished_subagents is orchestrator-only")
	}
	timeout := time.Duration(in.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = defaultWait
	}
	timeout = min(timeout, maxWait)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		changed := s.Log.Changed(a.SessionID) // before the check, so no change is missed
		scopes, err := s.pickScopes(a, in.Scope, true, true) // each round: the orchestrator's spawns may add scopes
		if err != nil {
			return nil, err
		}
		notes, err := s.Store.ListNotesIn(a.SessionID, boardIDs(scopes), NoteFilter{Type: in.Type, SinceID: in.SinceID})
		if err != nil {
			return nil, err
		}
		if notes == nil {
			notes = []Note{}
		}
		out := waitOut{Notes: notes}
		wake := len(notes) > 0
		if in.FinishedSubagents != nil {
			agents, err := s.Store.ListAgents(a.SessionID, "subagent")
			if err != nil {
				return nil, err
			}
			fin, run := 0, 0
			for _, ag := range agents {
				if ag.Status == "running" {
					run++
				} else {
					fin++
				}
			}
			out.FinishedSubagents, out.RunningSubagents = &fin, &run
			// With nothing running, no exit can come, so do not block for one.
			wake = wake || fin > *in.FinishedSubagents || run == 0
		}
		if wake {
			return out, nil
		}
		select {
		case <-changed:
		case <-timer.C:
			return out, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
