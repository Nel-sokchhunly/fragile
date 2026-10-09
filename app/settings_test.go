package main

import (
	"reflect"

	"github.com/Nel-sokchhunly/fragile/notes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	a, _ := newTestApp(t, dir, "")
	if s, err := a.GetSettings(); err != nil || s.AutoCompactTokens != 0 {
		t.Fatalf("default = %+v, %v", s, err)
	}
	for _, bad := range []int{-1, 1, 19_999, 1_000_001} {
		if err := a.SetSettings(Settings{AutoCompactTokens: bad}); err == nil {
			t.Errorf("%d accepted", bad)
		}
	}
	for _, ok := range []int{150_000, 20_000, 1_000_000, 0} {
		if err := a.SetSettings(Settings{AutoCompactTokens: ok}); err != nil {
			t.Fatalf("%d rejected: %v", ok, err)
		}
		if s, _ := a.GetSettings(); s.AutoCompactTokens != ok {
			t.Fatalf("got %+v after setting %d", s, ok)
		}
	}
	a.SetSettings(Settings{AutoCompactTokens: 90_000})
	a.close()

	// Persisted: a fresh app on the same data dir reads it from the DB.
	b, _ := newTestApp(t, dir, "")
	if s, err := b.GetSettings(); err != nil || s.AutoCompactTokens != 90_000 {
		t.Fatalf("after reopen = %+v, %v", s, err)
	}
}

func TestSettingsTemplate(t *testing.T) {
	dir := t.TempDir()
	a, _ := newTestApp(t, dir, "")
	defer a.close()

	// Never saved: every detected CLI (or Claude) is enabled.
	s, err := a.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !s.SubagentProviders[notes.ProviderClaude].Enabled && len(s.sessionConfig().EnabledProviders) == 0 {
		t.Fatalf("default enables nothing: %+v", s.SubagentProviders)
	}

	for _, bad := range []Settings{
		{SubagentProviders: SubagentProvidersSettings{"nope": {Enabled: true}}},
		{SubagentProviders: SubagentProvidersSettings{notes.ProviderClaude: {Enabled: false}}},
		{SubagentProviders: SubagentProvidersSettings{notes.ProviderAGY: {Enabled: true, DefaultModel: "sonnet"}}},
	} {
		if err := a.SetSettings(bad); err == nil {
			t.Errorf("SetSettings(%+v) accepted", bad)
		}
	}

	// Claude can be turned off as long as another CLI stays on.
	want := Settings{
		AutoCompactTokens: 80_000,
		OrchestratorRules: "gemini flash: docs and tests",
		SubagentProviders: SubagentProvidersSettings{
			notes.ProviderClaude: {Enabled: false, DefaultModel: "sonnet"},
			notes.ProviderAGY:    {Enabled: true, DefaultModel: "flash"},
		},
	}
	if err := a.SetSettings(want); err != nil {
		t.Fatal(err)
	}
	if m := a.subagentDefaultModel(notes.ProviderAGY); m != "flash" {
		t.Fatalf("default model = %q", m)
	}
	se, err := a.CreateSession("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(se.SessionConfig, notes.SessionConfig{EnabledProviders: []string{notes.ProviderAGY}, AutoCompactTokens: 80_000, OrchestratorRules: want.OrchestratorRules}) {
		t.Fatalf("session did not copy the template: %+v", se.SessionConfig)
	}

	// The template does not reach existing sessions.
	want.AutoCompactTokens = 0
	if err := a.SetSettings(want); err != nil {
		t.Fatal(err)
	}
	if se, _ = a.store.GetSession(se.ID); se.AutoCompactTokens != 80_000 {
		t.Fatalf("existing session changed: %+v", se.SessionConfig)
	}

	// Persisted.
	a.close()
	b, _ := newTestApp(t, dir, "")
	defer b.close()
	if got, err := b.GetSettings(); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("after reopen = %+v, %v", got, err)
	}
}

// Disabled plugins are dropped from the next spawn's plugin dirs without a restart.
func TestDisabledPlugins(t *testing.T) {
	claude := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	plug := filepath.Join(claude, "plugins", "cache", "a")
	if err := os.MkdirAll(plug, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(claude, "settings.json"), []byte(`{"enabledPlugins":{"a@m":true}}`), 0o644)
	os.WriteFile(filepath.Join(claude, "plugins", "installed_plugins.json"), []byte(`{"plugins":{"a@m":[{"installPath":"`+plug+`"}]}}`), 0o644)
	a, _ := newTestApp(t, t.TempDir(), "")
	defer a.close()
	if got := a.ListUserPlugins(); !reflect.DeepEqual(got, []string{"a@m"}) {
		t.Fatalf("ListUserPlugins = %v", got)
	}
	if got := a.subagentPluginDirs(); !reflect.DeepEqual(got, []string{plug}) {
		t.Fatalf("default dirs = %v", got)
	}
	if err := a.SetSettings(Settings{DisabledPlugins: []string{"a@m"}}); err != nil {
		t.Fatal(err)
	}
	if got := a.subagentPluginDirs(); len(got) != 0 {
		t.Fatalf("disabled plugin still loaded: %v", got)
	}
}

// autoCompactSession starts an orchestrator that answers /compact and records its stdin.
func autoCompactSession(t *testing.T, tokens, used int) (a *App, sid int64, stdinLog string) {
	t.Helper()
	a, _ = newTestApp(t, t.TempDir(), compactOrchestrator)
	work := t.TempDir()
	se, err := a.CreateSession("", work)
	if err != nil {
		t.Fatal(err)
	}
	orch, err := a.runner.StartOrchestrator(se.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetAgentContext(se.ID, orch.ID, used, defaultWindow); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetSessionConfig(se.ID, notes.SessionConfig{EnabledProviders: se.EnabledProviders, AutoCompactTokens: tokens}); err != nil {
		t.Fatal(err)
	}
	return a, se.ID, filepath.Join(work, "stdin.log")
}

func compactsSent(stdinLog string) int {
	b, _ := os.ReadFile(stdinLog)
	return strings.Count(string(b), `"text":"/compact"`)
}

func TestMaybeAutoCompact(t *testing.T) {
	t.Run("off", func(t *testing.T) {
		a, sid, log := autoCompactSession(t, 0, 500_000)
		if a.maybeAutoCompact(sid) || compactsSent(log) != 0 {
			t.Fatal("compacted while off")
		}
	})
	t.Run("below threshold", func(t *testing.T) {
		a, sid, log := autoCompactSession(t, 100_000, 99_999)
		if a.maybeAutoCompact(sid) || compactsSent(log) != 0 {
			t.Fatal("compacted below the threshold")
		}
	})
	t.Run("above threshold, never twice in a row", func(t *testing.T) {
		a, sid, log := autoCompactSession(t, 100_000, 100_000)
		if !a.maybeAutoCompact(sid) {
			t.Fatal("did not compact at the threshold")
		}
		waitFor(t, "compact to be sent", func() bool { return compactsSent(log) == 1 })
		orchs, _ := a.store.ListAgents(sid, "orchestrator")
		waitFor(t, "compaction to finish", func() bool {
			a.mu.Lock()
			busy := a.busy[sid]
			a.mu.Unlock()
			ag, err := a.store.GetAgent(sid, orchs[0].ID)
			return !busy && err == nil && ag.ContextUsed == 4736
		})
		// The compact's own result ends a turn, and the app's turn-end hook consumes the skip flag.
		waitFor(t, "the compact's turn end to be checked", func() bool {
			a.prefs.mu.Lock()
			defer a.prefs.mu.Unlock()
			return !a.prefs.lastAuto[sid]
		})
		if compactsSent(log) != 1 {
			t.Fatalf("compacts sent = %d, want 1", compactsSent(log))
		}
		// Had that result's check seen high context, it is still skipped.
		a.store.SetAgentContext(sid, orchs[0].ID, 150_000, defaultWindow)
		a.prefs.mu.Lock()
		a.prefs.lastAuto[sid] = true
		a.prefs.mu.Unlock()
		if a.maybeAutoCompact(sid) {
			t.Fatal("compacted again right after a compact")
		}
		// A normal turn later is checked again.
		a.store.SetAgentContext(sid, orchs[0].ID, 150_000, defaultWindow)
		if !a.maybeAutoCompact(sid) {
			t.Fatal("did not compact on a later turn")
		}
	})
	t.Run("already busy", func(t *testing.T) {
		a, sid, log := autoCompactSession(t, 20_000, 50_000)
		a.setBusy(sid, true)
		if a.maybeAutoCompact(sid) || compactsSent(log) != 0 {
			t.Fatal("compacted a busy orchestrator")
		}
		a.setBusy(sid, false)
		if !a.maybeAutoCompact(sid) { // the failed attempt left no skip flag behind
			t.Fatal("did not compact once idle")
		}
	})
}
