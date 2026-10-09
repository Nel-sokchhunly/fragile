package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// Each field of Settings is one row of the settings table, holding its JSON value.
const (
	keyAutoCompact         = "auto_compact_tokens"
	keyOrchestratorRules   = "orchestrator_rules"
	keyEscalationThreshold = "escalation_threshold"
	keySubagentProviders   = "subagent_providers"
	keyDisabledPlugins     = "subagent_disabled_plugins"
)

// SubagentProviderSetting is one CLI in the global template: whether new
// sessions enable it, and the model its sub-agents get when spawned without one.
type SubagentProviderSetting struct {
	Enabled      bool   `json:"enabled"`
	DefaultModel string `json:"default_model,omitempty"`
}

// SubagentProvidersSettings maps a CLI (notes.Providers) to its setting.
type SubagentProvidersSettings map[string]SubagentProviderSetting

// Settings are the user's app-wide preferences. Apart from the default models,
// they are a template: a new session copies them into its notes.SessionConfig,
// and changing them never affects existing sessions.
type Settings struct {
	AutoCompactTokens   int                       `json:"auto_compact_tokens"` // 0 = off (default)
	OrchestratorRules   string                    `json:"orchestrator_rules"`
	EscalationThreshold string                    `json:"escalation_threshold"`
	SubagentProviders   SubagentProvidersSettings `json:"subagent_providers"` // nil = never saved: GetSettings enables every detected CLI

	// DisabledPlugins are notes.UserPlugins names that newly spawned sub-agents
	// do not load. Global, read at spawn time; empty (default) loads them all.
	DisabledPlugins []string `json:"subagent_disabled_plugins"`
}

// sessionConfig is the template for a new session.
func (s Settings) sessionConfig() notes.SessionConfig {
	c := notes.SessionConfig{
		AutoCompactTokens:   s.AutoCompactTokens,
		OrchestratorRules:   s.OrchestratorRules,
		EscalationThreshold: s.EscalationThreshold,
	}
	for _, p := range notes.Providers {
		if s.SubagentProviders[p].Enabled {
			c.EnabledProviders = append(c.EnabledProviders, p)
		}
	}
	return c
}

func (s Settings) validate() error {
	for name, v := range s.SubagentProviders {
		if v.DefaultModel == "" {
			continue
		}
		if _, err := notes.ValidateModel(name, v.DefaultModel); err != nil {
			return fmt.Errorf("%s default model: %w", name, err)
		}
	}
	c := s.sessionConfig()
	if s.SubagentProviders == nil {
		c.EnabledProviders = []string{notes.ProviderClaude}
	}
	return c.Check()
}

// prefs caches the settings so per-spawn lookups don't hit the DB; it also
// holds which sessions' last turn was an auto-compact.
type prefs struct {
	mu       sync.Mutex
	loaded   bool
	cur      Settings
	lastAuto map[int64]bool
}

// settings returns the stored settings, without detecting CLIs.
func (a *App) settings() (Settings, error) {
	a.prefs.mu.Lock()
	defer a.prefs.mu.Unlock()
	if a.prefs.loaded {
		return a.prefs.cur, nil
	}
	var s Settings
	for key, dst := range map[string]any{keyAutoCompact: &s.AutoCompactTokens, keyOrchestratorRules: &s.OrchestratorRules, keyEscalationThreshold: &s.EscalationThreshold, keySubagentProviders: &s.SubagentProviders, keyDisabledPlugins: &s.DisabledPlugins} {
		v, err := a.store.GetSetting(key)
		if errors.Is(err, notes.ErrNotFound) {
			continue
		}
		if err != nil {
			return Settings{}, err
		}
		if err := json.Unmarshal([]byte(v), dst); err != nil {
			return Settings{}, fmt.Errorf("stored %s: %w", key, err)
		}
	}
	a.prefs.cur, a.prefs.loaded = s, true
	return s, nil
}

// GetSettings returns the settings, with defaults for anything never set.
// Until the CLI list is saved, every detected CLI is enabled (Claude if none is).
func (a *App) GetSettings() (Settings, error) {
	s, err := a.settings()
	if err != nil || s.SubagentProviders != nil {
		return s, err
	}
	s.SubagentProviders = SubagentProvidersSettings{notes.ProviderClaude: {Enabled: true}}
	for _, p := range notes.DetectProviders() {
		if p.Available {
			s.SubagentProviders[p.Name] = SubagentProviderSetting{Enabled: true}
		}
	}
	return s, nil
}

// SetSettings validates and stores the settings. They apply to sessions
// created afterwards; default models apply from the next spawn.
func (a *App) SetSettings(s Settings) error {
	if err := s.validate(); err != nil {
		return err
	}
	s.SubagentProviders = maps.Clone(s.SubagentProviders)
	s.DisabledPlugins = slices.Clone(s.DisabledPlugins)
	a.prefs.mu.Lock()
	defer a.prefs.mu.Unlock()
	for key, v := range map[string]any{keyAutoCompact: s.AutoCompactTokens, keyOrchestratorRules: s.OrchestratorRules, keyEscalationThreshold: s.EscalationThreshold, keySubagentProviders: s.SubagentProviders, keyDisabledPlugins: s.DisabledPlugins} {
		b, _ := json.Marshal(v)
		if err := a.store.PutSetting(key, string(b)); err != nil {
			a.prefs.loaded = false // some rows may be written; reload them
			return err
		}
	}
	a.prefs.cur, a.prefs.loaded = s, true
	return nil
}

// subagentPluginDirs is the Runner's PluginDirs: every user plugin except the
// disabled ones, read from the current settings on each spawn.
func (a *App) subagentPluginDirs() []string {
	s, _ := a.settings()
	var dirs []string
	for _, p := range notes.UserPlugins(a.agentDir) {
		if !slices.Contains(s.DisabledPlugins, p.Name) {
			dirs = append(dirs, p.Dir)
		}
	}
	return dirs
}

// ListUserPlugins returns the names of the plugins sub-agents can load.
func (a *App) ListUserPlugins() []string {
	names := []string{}
	for _, p := range notes.UserPlugins(a.agentDir) {
		names = append(names, p.Name)
	}
	return names
}

// subagentDefaultModel is the Runner's DefaultModel: the template's model for the CLI.
func (a *App) subagentDefaultModel(provider string) string {
	s, err := a.settings()
	if err != nil {
		return ""
	}
	return s.SubagentProviders[provider].DefaultModel
}
