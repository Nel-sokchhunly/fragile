package notes

import (
	"strings"
	"testing"
)

func TestSpawnSubagentProviderValidation(t *testing.T) {
	s, ts := newTestServer(t)
	dir := t.TempDir()
	s.Runner = NewRunner(Config{Addr: strings.TrimPrefix(ts.URL, "http://"), AgentDir: dir, WorkDir: dir}, s.Store, s.Log)
	s.Runner.Preflight = nil
	s.Runner.Command = writeFake(t, dir, `echo '{"type":"result"}'`)

	// Session with only claude and agy enabled
	sess, err := s.Store.CreateSessionWithProviders("multi-cli-test", dir, ProviderClaude, []string{ProviderClaude, ProviderAGY})
	if err != nil {
		t.Fatal(err)
	}
	orch, err := s.Store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	co := connect(t, ts, orch.Token)

	// 1. Unknown provider rejected
	if out, isErr := call(t, co, "spawn_subagent", map[string]any{"task": "test unknown", "provider": "invalid-cli"}); !isErr || !strings.Contains(out, "unknown provider") {
		t.Fatalf("expected unknown provider error, got: %s (isErr=%v)", out, isErr)
	}

	// 2. Disabled provider rejected (codex is not in enabled_providers)
	if out, isErr := call(t, co, "spawn_subagent", map[string]any{"task": "test disabled", "provider": ProviderCodex}); !isErr || !strings.Contains(out, "not enabled") {
		t.Fatalf("expected provider not enabled error, got: %s (isErr=%v)", out, isErr)
	}

	// 3. Enabled provider accepted (agy)
	out, isErr := call(t, co, "spawn_subagent", map[string]any{"task": "test agy", "provider": ProviderAGY, "model": "flash"})
	if isErr {
		t.Fatalf("spawn_subagent with enabled agy provider failed: %s", out)
	}

	// Verify agent has provider = agy
	subs, err := s.Store.ListAgents(sess.ID, "subagent")
	if err != nil || len(subs) != 1 {
		t.Fatalf("expected 1 subagent, got %v (err: %v)", len(subs), err)
	}
	if subs[0].Provider != ProviderAGY {
		t.Fatalf("expected agent provider %q, got %q", ProviderAGY, subs[0].Provider)
	}

	// 4. Empty provider defaults to orchestrator provider (claude)
	out, isErr = call(t, co, "spawn_subagent", map[string]any{"task": "test default provider"})
	if isErr {
		t.Fatalf("spawn_subagent with default provider failed: %s", out)
	}
	subs, err = s.Store.ListAgents(sess.ID, "subagent")
	if err != nil || len(subs) != 2 {
		t.Fatalf("expected 2 subagents, got %v (err: %v)", len(subs), err)
	}
	if subs[1].Provider != ProviderClaude {
		t.Fatalf("expected second agent provider %q, got %q", ProviderClaude, subs[1].Provider)
	}
}

func TestSpawnSubagentModelValidationPerProvider(t *testing.T) {
	s, ts := newTestServer(t)
	dir := t.TempDir()
	s.Runner = NewRunner(Config{Addr: strings.TrimPrefix(ts.URL, "http://"), AgentDir: dir, WorkDir: dir}, s.Store, s.Log)
	s.Runner.Preflight = nil
	s.Runner.Command = writeFake(t, dir, `echo '{"type":"result"}'`)
	s.Runner.CodexCommand = fakeCodex(t, "clean-exit")
	s.Runner.AGYCommand, _ = fakeAGY(t)

	// Session with all 3 providers enabled
	sess, err := s.Store.CreateSessionWithProviders("all-providers", dir, ProviderClaude, []string{ProviderClaude, ProviderCodex, ProviderAGY})
	if err != nil {
		t.Fatal(err)
	}
	orch, err := s.Store.CreateAgent(sess.ID, "orchestrator", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	co := connect(t, ts, orch.Token)

	// AGY rejects Claude model aliases
	if out, isErr := call(t, co, "spawn_subagent", map[string]any{"task": "test", "provider": ProviderAGY, "model": "sonnet"}); !isErr || !strings.Contains(out, "Claude Code alias") {
		t.Fatalf("AGY with 'sonnet' should fail: %s (isErr=%v)", out, isErr)
	}

	// AGY accepts flash alias
	if out, isErr := call(t, co, "spawn_subagent", map[string]any{"task": "test", "provider": ProviderAGY, "model": "flash"}); isErr {
		t.Fatalf("AGY with 'flash' should succeed: %s", out)
	}

	// Codex rejects Claude model aliases
	if out, isErr := call(t, co, "spawn_subagent", map[string]any{"task": "test", "provider": ProviderCodex, "model": "sonnet"}); !isErr || !strings.Contains(out, "Claude model") {
		t.Fatalf("Codex with 'sonnet' should fail: %s (isErr=%v)", out, isErr)
	}

	// Codex accepts valid model
	if out, isErr := call(t, co, "spawn_subagent", map[string]any{"task": "test", "provider": ProviderCodex, "model": "gpt-5-preview"}); isErr {
		t.Fatalf("Codex with valid model should succeed: %s", out)
	}

	// Claude accepts sonnet alias
	if out, isErr := call(t, co, "spawn_subagent", map[string]any{"task": "test", "provider": ProviderClaude, "model": "sonnet"}); isErr {
		t.Fatalf("Claude with 'sonnet' should succeed: %s", out)
	}
}

func TestPromptAvailableProviders(t *testing.T) {
	// FormatAvailableProviders formats descriptions and hints
	text := FormatAvailableProviders([]string{ProviderClaude, ProviderAGY})
	if !strings.Contains(text, "claude (Claude Code") {
		t.Errorf("expected claude in formatted providers: %s", text)
	}
	if !strings.Contains(text, "agy (Antigravity CLI") {
		t.Errorf("expected agy in formatted providers: %s", text)
	}
	if !strings.Contains(text, `Use spawn_subagent(..., provider="...") to specify a provider.`) {
		t.Errorf("expected usage hint in formatted providers: %s", text)
	}

	// OrchestratorPrompt injects providers
	prompt := OrchestratorPrompt("/test/workdir", false, ProviderClaude, ProviderCodex, ProviderAGY)
	if !strings.Contains(prompt, "Available sub-agent providers in this session:") {
		t.Errorf("prompt missing available providers line:\n%s", prompt)
	}
	if !strings.Contains(prompt, `Use spawn_subagent(..., provider="...") to specify a provider.`) {
		t.Errorf("prompt missing usage hint:\n%s", prompt)
	}
}
