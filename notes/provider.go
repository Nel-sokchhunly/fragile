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
)

func CheckProvider(p string) error {
	if p != ProviderClaude && p != ProviderCodex {
		return fmt.Errorf("unknown provider %q (choose claude or codex)", p)
	}
	return nil
}
func (r *Runner) provider(id int64) string {
	if s, e := r.store.GetSession(id); e == nil && s.Provider == ProviderCodex {
		return ProviderCodex
	}
	return ProviderClaude
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
		end = strings.Index(prompt, "5. **Wait loop.**")
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
