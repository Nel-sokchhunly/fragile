package notes

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderAdaptersRegistryAndDetection(t *testing.T) {
	// 1. Adapter lookup
	for _, name := range []string{ProviderClaude, ProviderCodex, ProviderAGY} {
		adapter, ok := GetAdapter(name)
		if !ok || adapter == nil {
			t.Fatalf("GetAdapter(%q) not found", name)
		}
		if adapter.Name() != name {
			t.Errorf("adapter.Name() = %q, want %q", adapter.Name(), name)
		}

		alias, ok := Adapter(name)
		if !ok || alias != adapter {
			t.Errorf("Adapter(%q) does not match GetAdapter", name)
		}
	}

	if _, ok := GetAdapter("unknown"); ok {
		t.Fatal("GetAdapter(unknown) should return false")
	}

	all := Adapters()
	if len(all) != 3 {
		t.Errorf("len(Adapters()) = %d, want 3", len(all))
	}

	// 2. DetectProviders
	infos := DetectProviders()
	if len(infos) != 3 {
		t.Fatalf("len(DetectProviders()) = %d, want 3", len(infos))
	}
	infoMap := make(map[string]ProviderInfo)
	for _, info := range infos {
		infoMap[info.Name] = info
	}

	for _, name := range []string{ProviderClaude, ProviderCodex, ProviderAGY} {
		info, ok := infoMap[name]
		if !ok {
			t.Errorf("DetectProviders missing %q", name)
			continue
		}
		if info.Available && info.Reason != "" {
			t.Errorf("available provider %q has reason %q", name, info.Reason)
		}
		if !info.Available && info.Reason == "" {
			t.Errorf("unavailable provider %q has empty reason", name)
		}
	}
}

func TestProviderModelValidation(t *testing.T) {
	claude, _ := GetAdapter(ProviderClaude)
	codex, _ := GetAdapter(ProviderCodex)
	agy, _ := GetAdapter(ProviderAGY)

	// Claude: resolves aliases, accepts claude- prefixed, rejects others
	if m, err := claude.ValidateModel("sonnet"); err != nil || m != "claude-sonnet-5-5" {
		t.Errorf("claude validate sonnet = %q, %v", m, err)
	}
	if m, err := claude.ValidateModel(""); err != nil || m != "" {
		t.Errorf("claude validate empty = %q, %v", m, err)
	}
	if m, err := claude.ValidateModel("claude-custom"); err != nil || m != "claude-custom" {
		t.Errorf("claude validate custom = %q, %v", m, err)
	}
	if _, err := claude.ValidateModel("gpt-4"); err == nil {
		t.Error("claude validate gpt-4 should error")
	}

	// Codex: rejects Claude models/aliases, accepts others
	if m, err := codex.ValidateModel("o3-mini"); err != nil || m != "o3-mini" {
		t.Errorf("codex validate o3-mini = %q, %v", m, err)
	}
	if m, err := codex.ValidateModel(""); err != nil || m != "" {
		t.Errorf("codex validate empty = %q, %v", m, err)
	}
	if _, err := codex.ValidateModel("sonnet"); err == nil {
		t.Error("codex validate sonnet should error")
	}
	if _, err := codex.ValidateModel("claude-3-7-sonnet"); err == nil {
		t.Error("codex validate claude- prefix should error")
	}

	// AGY: resolves aliases (flash -> gemini-3.8-flash-medium), rejects Claude aliases
	if m, err := agy.ValidateModel("flash"); err != nil || m != "gemini-3.8-flash-medium" {
		t.Errorf("agy validate flash = %q, %v", m, err)
	}
	if m, err := agy.ValidateModel(""); err != nil || m != "" {
		t.Errorf("agy validate empty = %q, %v", m, err)
	}
	if _, err := agy.ValidateModel("sonnet"); err == nil {
		t.Error("agy validate sonnet should error")
	}
}

func TestProviderAdjustPrompt(t *testing.T) {
	claude, _ := GetAdapter(ProviderClaude)
	codex, _ := GetAdapter(ProviderCodex)
	agy, _ := GetAdapter(ProviderAGY)

	basePrompt := "Task/Agent Claude Code The orchestrator is not sandboxed."
	a := Agent{Role: "subagent"}

	// Claude leaves prompt as is
	if got := claude.AdjustPrompt(a, basePrompt); got != basePrompt {
		t.Errorf("claude adjust prompt = %q, want %q", got, basePrompt)
	}

	// Codex modifies prompt
	codexGot := codex.AdjustPrompt(a, basePrompt)
	if !strings.Contains(codexGot, "spawn_agent") || !strings.Contains(codexGot, "The orchestrator is also sandboxed") {
		t.Errorf("unexpected codex adjusted prompt: %q", codexGot)
	}

	// AGY modifies prompt
	agyGot := agy.AdjustPrompt(a, basePrompt)
	if !strings.Contains(agyGot, "invoke_subagent") || !strings.Contains(agyGot, "Antigravity") {
		t.Errorf("unexpected agy adjusted prompt: %q", agyGot)
	}
}

func TestRunnerProviderResolution(t *testing.T) {
	// Without store (unit runner)
	r := &Runner{}
	if p := r.provider(1); p != ProviderClaude {
		t.Errorf("runner.provider without store = %q, want %q", p, ProviderClaude)
	}
	if p := r.ProviderForAgent(Agent{Provider: ProviderCodex}); p != ProviderCodex {
		t.Errorf("runner.ProviderForAgent = %q, want %q", p, ProviderCodex)
	}

	// With store
	s, err := OpenStore(filepath.Join(t.TempDir(), "runner_provider.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r.store = s

	sessClaude, _ := s.CreateSessionWithProvider("claude-sess", t.TempDir(), ProviderClaude)
	sessCodex, _ := s.CreateSessionWithProvider("codex-sess", t.TempDir(), ProviderCodex)

	// Orchestrators falling back to session provider
	orchClaude, _ := s.CreateAgent(sessClaude.ID, "orchestrator", 0, 0, "")
	orchCodex, _ := s.CreateAgent(sessCodex.ID, "orchestrator", 0, 0, "")

	// Subagent in claude session with explicit codex provider
	subCodexInClaude, _ := s.CreateAgent(sessClaude.ID, "subagent", orchClaude.ID, 0, ProviderCodex)
	// Subagent in codex session with explicit agy provider
	subAGYInCodex, _ := s.CreateAgent(sessCodex.ID, "subagent", orchCodex.ID, 0, ProviderAGY)

	// Test r.provider with session IDs
	if p := r.provider(sessClaude.ID); p != ProviderClaude {
		t.Errorf("provider(sessClaude.ID) = %q, want %q", p, ProviderClaude)
	}
	if p := r.provider(sessCodex.ID); p != ProviderCodex {
		t.Errorf("provider(sessCodex.ID) = %q, want %q", p, ProviderCodex)
	}

	// Test ProviderForAgent
	if p := r.ProviderForAgent(orchClaude); p != ProviderClaude {
		t.Errorf("ProviderForAgent(orchClaude) = %q, want %q", p, ProviderClaude)
	}
	if p := r.ProviderForAgent(orchCodex); p != ProviderCodex {
		t.Errorf("ProviderForAgent(orchCodex) = %q, want %q", p, ProviderCodex)
	}
	if p := r.ProviderForAgent(subCodexInClaude); p != ProviderCodex {
		t.Errorf("ProviderForAgent(subCodexInClaude) = %q, want %q", p, ProviderCodex)
	}
	if p := r.ProviderForAgent(subAGYInCodex); p != ProviderAGY {
		t.Errorf("ProviderForAgent(subAGYInCodex) = %q, want %q", p, ProviderAGY)
	}

	// Test ProviderForAgentID
	if p := r.ProviderForAgentID(subCodexInClaude.ID); p != ProviderCodex {
		t.Errorf("ProviderForAgentID(subCodexInClaude.ID) = %q, want %q", p, ProviderCodex)
	}
	if p := r.ProviderForAgentID(subAGYInCodex.ID); p != ProviderAGY {
		t.Errorf("ProviderForAgentID(subAGYInCodex.ID) = %q, want %q", p, ProviderAGY)
	}

	// Test r.Provider(target any) helper
	if p := r.Provider(subCodexInClaude); p != ProviderCodex {
		t.Errorf("Provider(Agent) = %q, want %q", p, ProviderCodex)
	}
	if p := r.Provider(&subAGYInCodex); p != ProviderAGY {
		t.Errorf("Provider(*Agent) = %q, want %q", p, ProviderAGY)
	}
	if p := r.Provider(sessCodex.ID); p != ProviderCodex {
		t.Errorf("Provider(sessionID) = %q, want %q", p, ProviderCodex)
	}
}
