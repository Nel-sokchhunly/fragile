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
	keySubagentProviders = "subagent_providers"
)

// SubagentProviderSetting specifies whether a sub-agent CLI provider is enabled
// and what default model to use for it.
type SubagentProviderSetting struct {
	Enabled      bool   `json:"enabled"`
	DefaultModel string `json:"default_model,omitempty"`
}

// SubagentProvidersSettings maps provider name to its setting.
type SubagentProvidersSettings map[string]SubagentProviderSetting

// Settings are the user's app-wide preferences.
type Settings struct {
	AutoCompactTokens int `json:"auto_compact_tokens"` // compact the orchestrator after a turn once its context reaches this; 0 = off (default)
}

// prefs caches the settings so per-turn checks don't hit the DB; it also holds
// which sessions' last turn was an auto-compact.
type prefs struct {
	mu                sync.Mutex
	loaded            bool
	cur               Settings
	lastAuto          map[int64]bool
	subagentLoaded    bool
	subagentProviders SubagentProvidersSettings
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

// DefaultSubagentProviderSettings returns sub-agent provider settings with
// detected providers enabled by default. If no providers are detected on PATH,
// Claude is enabled as a fallback.
func DefaultSubagentProviderSettings() SubagentProvidersSettings {
	res := make(SubagentProvidersSettings)
	hasAny := false
	for _, p := range notes.DetectProviders() {
		res[p.Name] = SubagentProviderSetting{
			Enabled:      p.Available,
			DefaultModel: p.DefaultModel,
		}
		if p.Available {
			hasAny = true
		}
	}
	if !hasAny {
		s := res[notes.ProviderClaude]
		s.Enabled = true
		res[notes.ProviderClaude] = s
	}
	return res
}

func cloneSubagentProvidersSettings(in SubagentProvidersSettings) SubagentProvidersSettings {
	if in == nil {
		return nil
	}
	out := make(SubagentProvidersSettings, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// GetSubagentProviderSettings returns global sub-agent CLI settings with defaults.
func (a *App) GetSubagentProviderSettings() (SubagentProvidersSettings, error) {
	a.prefs.mu.Lock()
	defer a.prefs.mu.Unlock()
	if a.prefs.subagentLoaded {
		return cloneSubagentProvidersSettings(a.prefs.subagentProviders), nil
	}
	val, err := a.store.GetSetting(keySubagentProviders)
	switch {
	case errors.Is(err, notes.ErrNotFound):
		defaults := DefaultSubagentProviderSettings()
		a.prefs.subagentProviders = defaults
		a.prefs.subagentLoaded = true
		return cloneSubagentProvidersSettings(defaults), nil
	case err != nil:
		return nil, err
	default:
		var s SubagentProvidersSettings
		if err := json.Unmarshal([]byte(val), &s); err != nil {
			return nil, fmt.Errorf("stored %s: %w", keySubagentProviders, err)
		}
		if s == nil {
			s = make(SubagentProvidersSettings)
		}
		a.prefs.subagentProviders = s
		a.prefs.subagentLoaded = true
		return cloneSubagentProvidersSettings(s), nil
	}
}

// SetSubagentProviderSettings validates and stores global sub-agent CLI settings.
func (a *App) SetSubagentProviderSettings(s SubagentProvidersSettings) error {
	if s == nil {
		s = make(SubagentProvidersSettings)
	}
	for name := range s {
		if err := notes.CheckProvider(name); err != nil {
			return err
		}
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	a.prefs.mu.Lock()
	defer a.prefs.mu.Unlock()
	if err := a.store.PutSetting(keySubagentProviders, string(b)); err != nil {
		return err
	}
	a.prefs.subagentProviders = cloneSubagentProvidersSettings(s)
	a.prefs.subagentLoaded = true
	return nil
}
