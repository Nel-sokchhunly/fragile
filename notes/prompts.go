package notes

import (
	_ "embed"
	"strconv"
	"strings"
)

//go:embed prompts/orchestrator.md
var orchestratorPrompt string

//go:embed prompts/subagent.md
var subagentPrompt string

// OrchestratorPrompt returns the system prompt appended for the orchestrator.
func OrchestratorPrompt() string { return orchestratorPrompt }

// SubagentPrompt returns the system prompt appended for a sub-agent, with its
// agent ID and task substituted in.
func SubagentPrompt(agentID int64, task string) string {
	return strings.NewReplacer(
		"{{AGENT_ID}}", strconv.FormatInt(agentID, 10),
		"{{TASK}}", task,
	).Replace(subagentPrompt)
}
