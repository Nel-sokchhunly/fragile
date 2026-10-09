package main

import (
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

func TestSubagentProviderSettings(t *testing.T) {
	dir := t.TempDir()
	a, _ := newTestApp(t, dir, "")
	defer a.close()

	// Default settings reflect detected providers.
	defaults, err := a.GetSubagentProviderSettings()
	if err != nil {
		t.Fatalf("GetSubagentProviderSettings defaults error: %v", err)
	}
	for _, p := range a.GetProviders() {
		setting, ok := defaults[p.Name]
		if !ok {
			t.Errorf("provider %s missing from default settings", p.Name)
		}
		if p.Available && !setting.Enabled {
			t.Errorf("expected available provider %s to be enabled by default", p.Name)
		}
	}

	// Invalid provider name rejected
	bad := SubagentProvidersSettings{
		"invalid-prov": {Enabled: true},
	}
	if err := a.SetSubagentProviderSettings(bad); err == nil {
		t.Fatal("expected error for invalid provider name")
	}

	// Valid settings stored and retrieved
	updated := SubagentProvidersSettings{
		"claude": {Enabled: true, DefaultModel: "claude-sonnet-4-6"},
		"codex":  {Enabled: false},
		"agy":    {Enabled: true, DefaultModel: "flash"},
	}
	if err := a.SetSubagentProviderSettings(updated); err != nil {
		t.Fatalf("SetSubagentProviderSettings failed: %v", err)
	}
	got, err := a.GetSubagentProviderSettings()
	if err != nil {
		t.Fatalf("GetSubagentProviderSettings failed: %v", err)
	}
	if !got["claude"].Enabled || got["claude"].DefaultModel != "claude-sonnet-4-6" {
		t.Fatalf("unexpected claude setting: %+v", got["claude"])
	}
	if got["codex"].Enabled {
		t.Fatalf("expected codex to be disabled: %+v", got["codex"])
	}
	if !got["agy"].Enabled || got["agy"].DefaultModel != "flash" {
		t.Fatalf("unexpected agy setting: %+v", got["agy"])
	}

	// Persisted after restart
	a.close()
	b, _ := newTestApp(t, dir, "")
	defer b.close()
	reopened, err := b.GetSubagentProviderSettings()
	if err != nil {
		t.Fatalf("reopened GetSubagentProviderSettings failed: %v", err)
	}
	if !reopened["claude"].Enabled || reopened["claude"].DefaultModel != "claude-sonnet-4-6" {
		t.Fatalf("reopened claude setting: %+v", reopened["claude"])
	}
	if reopened["codex"].Enabled {
		t.Fatalf("reopened codex setting: %+v", reopened["codex"])
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
	if err := a.SetSettings(Settings{AutoCompactTokens: tokens}); err != nil {
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
