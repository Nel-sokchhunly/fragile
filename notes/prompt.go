package notes

import (
	"slices"
	"strings"
)

// availableProviders keeps the enabled CLIs that are installed and usable now.
func availableProviders(enabled []string) []string {
	var out []string
	for _, p := range DetectProviders() {
		if p.Available && slices.Contains(enabled, p.Name) {
			out = append(out, p.Name)
		}
	}
	return out
}

// sessionPromptText is the orchestrator prompt's {{PROVIDERS}} part: the
// session's usable sub-agent CLIs (enabled and installed) and the user's rules.
func sessionPromptText(se Session) string {
	def := ""
	if len(se.EnabledProviders) > 0 {
		def = se.EnabledProviders[0]
	}
	text := providersText(availableProviders(se.EnabledProviders), def)
	if rules := strings.TrimSpace(se.OrchestratorRules); rules != "" {
		text += "\n\n**The user's rules for this session** (they override the defaults above, including which CLI and model to pick):\n\n" + rules
	}
	return text
}

// providersText lists the sub-agent CLIs; def is the CLI spawn_subagent uses
// when provider is omitted.
func providersText(providers []string, def string) string {
	if len(providers) == 0 {
		return "No sub-agent CLI enabled in this session is installed and logged in. Do not spawn sub-agents; escalate to the user to enable or install one."
	}
	var hints []string
	for _, p := range providers {
		hints = append(hints, "`"+providerSpecs[p].hint+"`")
	}
	return "Enabled in this session: " + strings.Join(hints, ", ") + ". Pick one with `spawn_subagent(provider=...)`; omitted means " + "`" + def + "`" + ". Any other CLI is rejected; if a CLI fails to start, use another one from this list."
}
