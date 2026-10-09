package notes

import (
	"fmt"
	"strings"
)

// AvailableSubagentProviders returns provider info for providers that are both
// enabled in the session and detected on the host. If enabled is empty, it uses the
// detected providers; if none are detected, it falls back to claude.
func AvailableSubagentProviders(enabled []string) []ProviderInfo {
	detected := DetectProviders()
	detectedMap := make(map[string]ProviderInfo, len(detected))
	for _, p := range detected {
		if p.Available {
			detectedMap[p.Name] = p
		}
	}

	if len(enabled) == 0 {
		for _, name := range []string{ProviderClaude, ProviderCodex, ProviderAGY} {
			if info, ok := detectedMap[name]; ok {
				enabled = append(enabled, info.Name)
			}
		}
		if len(enabled) == 0 {
			enabled = []string{ProviderClaude}
		}
	}

	var out []ProviderInfo
	for _, name := range enabled {
		if info, ok := detectedMap[name]; ok {
			out = append(out, info)
		}
	}
	return out
}

// FormatAvailableProviders formats a descriptive list and usage hint for the
// enabled and detected sub-agent providers.
func FormatAvailableProviders(providers []string) string {
	if len(providers) == 0 {
		return "Available sub-agent providers in this session: none. Use spawn_subagent(..., provider=\"...\") to specify a provider."
	}
	var descs []string
	for _, p := range providers {
		switch p {
		case ProviderClaude:
			descs = append(descs, "claude (Claude Code; models: sonnet, opus, haiku)")
		case ProviderCodex:
			descs = append(descs, "codex (Codex CLI)")
		case ProviderAGY:
			descs = append(descs, "agy (Antigravity CLI; models: flash, pro, flash_lite)")
		default:
			descs = append(descs, p)
		}
	}
	return fmt.Sprintf("Available sub-agent providers in this session: %s. Use spawn_subagent(..., provider=\"...\") to specify a provider.", strings.Join(descs, ", "))
}
