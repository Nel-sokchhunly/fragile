package main

import (
	"encoding/json"
	"errors"
	"log"
	"strings"

	"github.com/Nel-sokchhunly/fragile/notes"
)

const (
	evRateLimit       = "rate_limit"         // the raw rate_limit_event line, stored when the limits change
	eventRateLimit    = "rate_limit_changed" // payload RateLimit
	defaultWindow     = 200_000
	longContextWindow = 1_000_000 // a model name ending in [1m]
)

// ctxInfo is what one stream-json line says about an agent's context; zero fields say nothing.
type ctxInfo struct {
	Used   int    // input + cache tokens of an assistant message (the context as of that message), or a compaction's post_tokens
	Model  string // from the system init line
	Window int    // from the model name's [1m] suffix, or a result's modelUsage
}

// parseContext reads context facts from one line. model is the agent's init model, to pick its entry
// out of a result's modelUsage.
func parseContext(line []byte, model string) (c ctxInfo) {
	var l struct {
		Type          string `json:"type"`
		Subtype       string `json:"subtype"`
		Provider      string `json:"provider"`
		ContextUsed   int    `json:"context_used"`
		ContextWindow int    `json:"context_window"`
		Model         string `json:"model"`
		Compact       struct {
			PostTokens int `json:"post_tokens"`
		} `json:"compact_metadata"`
		Message struct {
			Usage struct {
				Input   int `json:"input_tokens"`
				Created int `json:"cache_creation_input_tokens"`
				Read    int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		ModelUsage map[string]struct {
			ContextWindow int `json:"contextWindow"`
		} `json:"modelUsage"`
	}
	if json.Unmarshal(line, &l) != nil {
		return c
	}
	switch l.Type {
	case "system":
		switch {
		case l.Subtype == "init" && l.Model != "":
			c.Model, c.Window = l.Model, defaultWindow
			if l.Provider == notes.ProviderCodex {
				c.Window = 0
			}
			if strings.HasSuffix(l.Model, "[1m]") {
				c.Window = longContextWindow
			}
		case l.Subtype == "context_usage":
			c.Used, c.Window = l.ContextUsed, l.ContextWindow
		case l.Subtype == "compact_boundary": // no assistant message follows a /compact
			c.Used = l.Compact.PostTokens
		}
	case "assistant":
		u := l.Message.Usage
		c.Used = u.Input + u.Created + u.Read
	case "result":
		base := strings.TrimSuffix(model, "[1m]")
		for name, mu := range l.ModelUsage {
			if strings.TrimSuffix(name, "[1m]") == base && mu.ContextWindow > 0 {
				c.Window = mu.ContextWindow
			}
		}
	}
	return c
}

// LimitWindow is one subscription window: utilization 0..1, resets_at unix seconds.
type LimitWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    int64   `json:"resets_at"`
}

// RateLimit is the account-wide subscription limit state (nil window = not reported yet).
type RateLimit struct {
	FiveHour *LimitWindow `json:"five_hour"`
	SevenDay *LimitWindow `json:"seven_day"`
}

// parseRateLimit reads a rate_limit_event line; ok is false for any other line.
func parseRateLimit(line []byte) (r RateLimit, ok bool) {
	var l struct {
		Type string `json:"type"`
		Info struct {
			Windows map[string]struct {
				Utilization float64 `json:"utilization"`
				ResetsAt    int64   `json:"resetsAt"`
			} `json:"unifiedWindows"`
		} `json:"rate_limit_info"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "rate_limit_event" {
		return r, false
	}
	pick := func(k string) *LimitWindow {
		if w, ok := l.Info.Windows[k]; ok {
			return &LimitWindow{w.Utilization, w.ResetsAt}
		}
		return nil
	}
	r.FiveHour, r.SevenDay = pick("five_hour"), pick("seven_day")
	return r, r.FiveHour != nil || r.SevenDay != nil
}

func (r RateLimit) same(o RateLimit) bool {
	eq := func(a, b *LimitWindow) bool { return a == b || a != nil && b != nil && *a == *b }
	return eq(r.FiveHour, o.FiveHour) && eq(r.SevenDay, o.SevenDay)
}

// GetRateLimit returns the latest subscription limits seen (null until any agent has reported them).
func (a *App) GetRateLimit() *RateLimit {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.limit
}

// loadRateLimit seeds a.limit from the newest stored rate_limit event.
func (a *App) loadRateLimit() {
	if ev, err := a.store.LastAgentEventOfType(evRateLimit); err == nil {
		if r, ok := parseRateLimit([]byte(ev.Payload)); ok {
			a.limit = &r
		}
	} else if !errors.Is(err, notes.ErrNotFound) {
		log.Printf("loading the rate limit: %v", err)
	}
}

// trackUsage (called for every output line) keeps the agent's model, context usage and the global limits current.
func (a *App) trackUsage(ag notes.Agent, line []byte) {
	if r, ok := parseRateLimit(line); ok {
		a.mu.Lock()
		if a.limit != nil { // an event may report one window only: keep the other
			if r.FiveHour == nil {
				r.FiveHour = a.limit.FiveHour
			}
			if r.SevenDay == nil {
				r.SevenDay = a.limit.SevenDay
			}
		}
		changed := a.limit == nil || !a.limit.same(r)
		if changed {
			a.limit = &r
		}
		a.mu.Unlock()
		if changed {
			a.record(ag, evRateLimit, string(line))
			a.pushEvent(eventRateLimit, ag.SessionID, ag.ID, r)
		}
		return
	}
	a.mu.Lock()
	model := a.models[ag.ID]
	a.mu.Unlock()
	c := parseContext(line, model)
	if c == (ctxInfo{}) {
		return
	}
	if c.Model != "" {
		a.mu.Lock()
		a.models[ag.ID] = c.Model
		a.mu.Unlock()
	}
	cur, err := a.store.GetAgent(ag.SessionID, ag.ID)
	if err != nil {
		return
	}
	used, window := cur.ContextUsed, cur.ContextWindow
	if c.Used > 0 {
		used = c.Used
	}
	if c.Window > 0 {
		window = c.Window
	}
	if window == 0 {
		if se, err := a.store.GetSession(ag.SessionID); err == nil && se.Provider != notes.ProviderCodex {
			window = defaultWindow
		}
	}
	changed := false
	if c.Model != "" && c.Model != cur.Model && a.store.SetAgentModel(ag.SessionID, ag.ID, c.Model) == nil {
		cur.Model, changed = c.Model, true
	}
	if (used != cur.ContextUsed || window != cur.ContextWindow) && a.store.SetAgentContext(ag.SessionID, ag.ID, used, window) == nil {
		cur.ContextUsed, cur.ContextWindow, changed = used, window, true
	}
	if changed {
		a.pushEvent(eventAgentUpdated, ag.SessionID, ag.ID, cur)
	}
}
