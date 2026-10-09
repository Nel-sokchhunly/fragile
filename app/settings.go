package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/Nel-sokchhunly/fragile/notes"
)

const (
	minAutoCompactTokens = 20_000
	maxAutoCompactTokens = 1_000_000
	keyAutoCompact       = "auto_compact_tokens" // each field is one row of the settings table, holding its JSON value
)

// Settings are the user's app-wide preferences.
type Settings struct {
	AutoCompactTokens int `json:"auto_compact_tokens"` // compact the orchestrator after a turn once its context reaches this; 0 = off (default)
}

// prefs caches the settings so per-turn checks don't hit the DB; it also holds
// which sessions' last turn was an auto-compact.
type prefs struct {
	mu       sync.Mutex
	loaded   bool
	cur      Settings
	lastAuto map[int64]bool
}

func (s Settings) validate() error {
	if n := s.AutoCompactTokens; n != 0 && (n < minAutoCompactTokens || n > maxAutoCompactTokens) {
		return fmt.Errorf("auto-compact threshold must be 0 (off) or between %d and %d tokens", minAutoCompactTokens, maxAutoCompactTokens)
	}
	return nil
}

// GetSettings returns the settings, with defaults for anything never set.
func (a *App) GetSettings() (Settings, error) {
	a.prefs.mu.Lock()
	defer a.prefs.mu.Unlock()
	if a.prefs.loaded {
		return a.prefs.cur, nil
	}
	var s Settings
	switch v, err := a.store.GetSetting(keyAutoCompact); {
	case errors.Is(err, notes.ErrNotFound):
	case err != nil:
		return Settings{}, err
	default:
		n, err := strconv.Atoi(v)
		if err != nil {
			return Settings{}, fmt.Errorf("stored %s: %w", keyAutoCompact, err)
		}
		s.AutoCompactTokens = n
	}
	a.prefs.cur, a.prefs.loaded = s, true
	return s, nil
}

// SetSettings validates and stores the settings; they apply from the next turn.
func (a *App) SetSettings(s Settings) error {
	if err := s.validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(s.AutoCompactTokens)
	a.prefs.mu.Lock()
	defer a.prefs.mu.Unlock()
	if err := a.store.PutSetting(keyAutoCompact, string(b)); err != nil {
		return err
	}
	a.prefs.cur, a.prefs.loaded = s, true
	return nil
}
