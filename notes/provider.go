package notes

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
	ProviderAGY    = "agy"
)

func CheckProvider(p string) error {
	if p != ProviderClaude && p != ProviderCodex && p != ProviderAGY {
		return fmt.Errorf("unknown provider %q (choose claude, codex or agy)", p)
	}
	return nil
}

// provider is the session's own CLI: the orchestrator's, and the default for its sub-agents.
func (r *Runner) provider(sessionID int64) string {
	if r.store == nil {
		return ProviderClaude // arg-only test runners
	}
	if s, e := r.store.GetSession(sessionID); e == nil && s.Provider != "" {
		return s.Provider
	}
	return ProviderClaude
}

// ProviderForAgent is the CLI the agent runs on: its own, else its session's.
func (r *Runner) ProviderForAgent(a Agent) string {
	if a.Provider != "" {
		return a.Provider
	}
	return r.provider(a.SessionID)
}

// providerSpec is what Fragile needs to know about a CLI beyond launching it
// (start, startCodex, startAGY).
type providerSpec struct {
	detect func() error                 // on PATH, recent enough, logged in where checkable
	model  func(string) (string, error) // validates or expands a spawn_subagent model
	hint   string                       // one line for the orchestrator prompt
}

var providerSpecs = map[string]providerSpec{
	ProviderClaude: {
		detect: func() error {
			_, err := exec.LookPath("claude")
			if err != nil {
				return errors.New("claude CLI not found on PATH; install Claude Code and log in")
			}
			return nil
		},
		model: resolveModel,
		hint:  "claude (Claude Code; models: sonnet, opus, haiku)",
	},
	ProviderCodex: {
		detect: func() error { return checkCodexPrereqs("codex") },
		model:  func(m string) (string, error) { return m, validCodexModel(m) },
		hint:   "codex (Codex CLI; model: a full Codex model id, or omit)",
	},
	ProviderAGY: {
		detect: func() error { return checkAGYPrereqs("agy") },
		model:  resolveAGYModel,
		hint:   "agy (Antigravity CLI; models: flash, pro, flash_lite)",
	},
}

// ValidateModel checks (and expands an alias of) a model for the CLI.
func ValidateModel(provider, model string) (string, error) {
	if err := CheckProvider(provider); err != nil {
		return "", err
	}
	return providerSpecs[provider].model(model)
}

// Providers lists every known CLI in a fixed order.
var Providers = []string{ProviderClaude, ProviderCodex, ProviderAGY}

// ProviderInfo is a CLI's detection result; Reason says how to fix an unavailable one.
type ProviderInfo struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// DetectProviders checks each known CLI. It runs their --version, so call it
// on demand (Settings, session create, orchestrator launch), not per spawn.
func DetectProviders() []ProviderInfo {
	out := make([]ProviderInfo, len(Providers))
	for i, name := range Providers {
		out[i] = ProviderInfo{Name: name, Available: true}
		if err := providerSpecs[name].detect(); err != nil {
			out[i].Available, out[i].Reason = false, err.Error()
		}
	}
	return out
}

// Codex permission profiles require a recent CLI. Older CLIs must fail closed,
// not silently ignore deny-read rules. --strict-config also checks every launch.
func checkCodexPrereqs(command string) error {
	if _, e := exec.LookPath(command); e != nil {
		return errors.New("Codex CLI not found on PATH; install Codex CLI and run codex login with ChatGPT")
	}
	out, e := exec.Command(command, "--version").Output()
	if e != nil {
		return fmt.Errorf("checking Codex CLI: %w", e)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return errors.New("cannot identify Codex CLI version")
	}
	var major, minor int
	if _, e := fmt.Sscanf(fields[len(fields)-1], "%d.%d", &major, &minor); e != nil || (major == 0 && minor < 158) {
		return errors.New("Codex CLI 0.158.0 or newer is required for secret-read protection; update Codex CLI")
	}
	return nil
}

func codexEnv(environ []string) []string {
	out := agentEnv(environ, false)
	out = slices.DeleteFunc(out, func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains([]string{"OPENAI_API_KEY", "CODEX_API_KEY", "OPENAI_BASE_URL"}, k)
	})
	return out
}

// Explicit deny entries apply even when the store or credentials sit inside
// the project. The Codex process can authenticate normally; sandboxed tools
// cannot read its auth, Fragile tokens, SQLite sidecars, or personal credentials.
func codexArgs(cfg Config, dir, marker string) []string {
	fs := map[string]string{"/": "read", realPath(dir): "write"}
	home, _ := os.UserHomeDir()
	expand := func(p string) string {
		if strings.HasPrefix(p, "~/") {
			p = filepath.Join(home, p[2:])
		}
		return realPath(p)
	}
	for _, p := range sandboxCaches {
		fs[expand(p)] = "write"
	}
	if p, e := os.UserCacheDir(); e == nil {
		fs[realPath(p)] = "write"
	}
	hidden := append(slices.Clone(sandboxSecretFiles), "~/.claude", "~/.agents", cfg.AgentDir, cfg.DBPath, cfg.LogPath)
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	// Deny the complete state tree, including future log/database names. Only
	// installed executables may be read by the sandbox helper.
	hidden = append(hidden, codexHome)
	fs[expand(filepath.Join(codexHome, "packages"))] = "read"
	for _, p := range hidden {
		if p != "" {
			fs[expand(p)] = "deny"
		}
	}
	for _, p := range []string{cfg.DBPath + "-wal", cfg.DBPath + "-shm"} {
		if cfg.DBPath != "" {
			fs[realPath(p)] = "deny"
		}
	}
	keys := make([]string, 0, len(fs))
	for k := range fs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var entries []string
	for _, k := range keys {
		entries = append(entries, strconv.Quote(globEscape(k))+"="+strconv.Quote(fs[k]))
	}
	domains := []string{}
	for _, d := range sandboxDomains {
		domains = append(domains, strconv.Quote(d)+`="allow"`)
	}
	args := []string{"app-server", "--strict-config",
		"-c", `default_permissions="fragile"`,
		"-c", "permissions.fragile.filesystem={" + strings.Join(entries, ",") + "}",
		"-c", `permissions.fragile.network.enabled=true`,
		"-c", `permissions.fragile.network.mode="limited"`,
		"-c", "permissions.fragile.network.domains={" + strings.Join(domains, ",") + "}",
		"-c", `features.network_proxy=true`, "-c", `features.hooks=false`, "-c", `web_search="disabled"`,
		"-c", `features.multi_agent=false`, "-c", `features.plugins=false`, "-c", `features.apps=false`, "-c", `features.memories=false`,
		"-c", `forced_login_method="chatgpt"`, "-c", `model_provider="openai"`,
		"-c", `approval_policy="never"`, "-c", `shell_environment_policy.ignore_default_excludes=false`,
		"-c", `mcp_servers={fragile={url="http://127.0.0.1",enabled=false,name=` + strconv.Quote(marker) + `,default_tools_approval_mode="approve",tool_timeout_sec=180}}`,
	}
	return args
}

// Existing prompts describe Claude-specific tool names, permissions and models.
// Replace those sections, keeping team lifecycle and escalation semantics.
func codexPrompt(a Agent, prompt string) string {
	prompt = strings.ReplaceAll(prompt, "Task/Agent", "spawn_agent")
	prompt = strings.ReplaceAll(prompt, "The orchestrator is not sandboxed.", "The orchestrator is also sandboxed and may need to escalate a denied action to the user.")
	if a.Role == roleOrchestrator {
		start := strings.Index(prompt, "## Sandbox")
		end := strings.Index(prompt, "## Hard rules")
		if start >= 0 && end > start {
			prompt = prompt[:start] + "## Sandbox\n\nAll Codex agents, including you, are sandboxed: project/cache writes only, credential and Fragile-state reads denied, and command networking limited to package registries and GitHub. A denied action must be escalated with its exact command; do not bypass the sandbox.\n\n" + prompt[end:]
		}
		prompt = strings.ReplaceAll(prompt, "Claude Code", "Codex")
		prompt = strings.ReplaceAll(prompt, "Each sub-agent is a separate headless Codex process.", "Each sub-agent is a separate Codex app-server process.")
		start = strings.Index(prompt, "4. **Spawn**")
		end = strings.Index(prompt, "5. **Wait") // both the one-shot loop and the interactive variant
		if start >= 0 && end > start {
			prompt = prompt[:start] + "4. **Spawn** independent sub-agents with spawn_subagent. Omit model to use the installed Codex default; if selecting a model, use a full Codex model id available to your account, never Claude aliases.\n" + prompt[end:]
		}
		lines := strings.Split(prompt, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "- `spawn_subagent(") {
				lines[i] = "- `spawn_subagent(title, task, scopes, model?)` - use `scopes: [\"session\"]`. Always provide a short title and a self-contained task. Omit model for the authenticated Codex catalog default, or pass a full available Codex model id. Never use Claude aliases."
			}
		}
		prompt = strings.Join(lines, "\n")
	}
	return prompt + "\n\nUse only the Fragile MCP server for team coordination. Native delegation is disabled. Do not spawn agents with shell commands, contact unrelated MCP servers, or read personal skills, hooks, or memories.\n"
}

// checkAGYPrereqs checks for the Antigravity CLI binary and login.
func checkAGYPrereqs(command string) error {
	if _, e := exec.LookPath(command); e != nil {
		return errors.New("Antigravity CLI (agy) not found on PATH; install agy and log in")
	}
	out, e := exec.Command(command, "--version").Output()
	if e != nil {
		return fmt.Errorf("checking Antigravity CLI: %w", e)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return errors.New("cannot identify Antigravity CLI version")
	}
	return nil
}

// agyEnv cleans billing and API key overrides so agy uses standard account
// authentication, and points HOME at the agent's own tree (see agyHome).
func agyEnv(environ []string, home string) []string {
	out := agentEnv(environ, false)
	out = slices.DeleteFunc(out, func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains([]string{
			"GEMINI_API_KEY", "GOOGLE_API_KEY", "ANTIGRAVITY_API_KEY",
			"AGY_API_KEY", "VERTEX_API_KEY", "VERTEXAI_API_KEY", "HOME",
		}, k)
	})
	// Commands the agent runs keep the user's caches and git identity instead of
	// starting empty under the agent's HOME.
	if user, err := os.UserHomeDir(); err == nil {
		for k, v := range map[string]string{"GOPATH": filepath.Join(user, "go"), "XDG_CACHE_HOME": filepath.Join(user, ".cache"), "GIT_CONFIG_GLOBAL": filepath.Join(user, ".gitconfig")} {
			if !slices.ContainsFunc(out, func(kv string) bool { return strings.HasPrefix(kv, k+"=") }) {
				out = append(out, k+"="+v)
			}
		}
	}
	return append(out, "HOME="+home)
}

// agyModelAliases maps shorthand model names to Antigravity model ids (`agy models`).
var agyModelAliases = map[string]string{
	"flash":      "gemini-3.8-flash-medium",
	"pro":        "gemini-3.1-pro-high",
	"flash_lite": "gemini-3.8-flash-low",
	"flash-lite": "gemini-3.8-flash-low",
}

// resolveAGYModel resolves an AGY model alias to a full model id, or verifies a
// custom one. Full Claude ids agy serves (claude-sonnet-4-6) are allowed; the
// Claude Code aliases are not.
func resolveAGYModel(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "sonnet" || model == "opus" || model == "haiku" {
		return "", fmt.Errorf("%s is a Claude Code alias; omit model for the AGY default or pass an AGY model id", strconv.Quote(model))
	}
	if id, ok := agyModelAliases[model]; ok {
		return id, nil
	}
	if strings.ContainsAny(model, " \t\n") {
		return "", fmt.Errorf("invalid model %q: model id must not contain whitespace", model)
	}
	return model, nil
}

// validAGYModel checks whether model is allowed for an AGY agent.
func validAGYModel(model string) error {
	_, err := resolveAGYModel(model)
	return err
}

// agyPrompt tailors system prompts for Antigravity agents.
func agyPrompt(a Agent, prompt string) string {
	prompt = strings.ReplaceAll(prompt, "Task/Agent", "invoke_subagent")
	prompt = strings.ReplaceAll(prompt, "Claude Code", "Antigravity")
	prompt = strings.ReplaceAll(prompt, "The orchestrator is not sandboxed.", "The orchestrator also runs under the Antigravity terminal sandbox and may need to escalate a denied action to the user.")
	if a.Role == roleOrchestrator {
		start := strings.Index(prompt, "4. **Spawn**")
		end := strings.Index(prompt, "5. **Wait") // both the one-shot loop and the interactive variant
		if start >= 0 && end > start {
			prompt = prompt[:start] + "4. **Spawn** independent sub-agents with spawn_subagent. Omit model to use the Antigravity default, or pass an AGY model id (e.g. \"flash\", \"pro\", \"flash_lite\").\n" + prompt[end:]
		}
		lines := strings.Split(prompt, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "- `spawn_subagent(") {
				lines[i] = "- `spawn_subagent(title, task, scopes, model?)` - use `scopes: [\"session\"]`. Always provide a short title and a self-contained task. Omit model for the Antigravity default, or pass an AGY model id (e.g. \"flash\", \"pro\", \"flash_lite\"). Never use Claude aliases."
			}
		}
		prompt = strings.Join(lines, "\n")
	}
	return prompt + "\n\nUse only the Fragile MCP server for team coordination. Native delegation is disabled. Do not invoke subagents natively.\n"
}
