package notes

import (
	_ "embed"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed prompts/orchestrator.md
var orchestratorPrompt string

//go:embed prompts/subagent.md
var subagentPrompt string

// OrchestratorPrompt returns the system prompt appended for the orchestrator,
// with the absolute working directory substituted in.
func OrchestratorPrompt(workDir string) string {
	return strings.NewReplacer("{{WORKDIR}}", absDir(workDir)).Replace(orchestratorPrompt)
}

func absDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// SubagentPrompt returns the system prompt appended for a sub-agent, with its
// agent ID, task and absolute working directory substituted in (a Replacer
// does not rescan substituted text, so a task cannot inject placeholders).
func SubagentPrompt(agentID int64, task, workDir string) string {
	return strings.NewReplacer(
		"{{AGENT_ID}}", strconv.FormatInt(agentID, 10),
		"{{TASK}}", task,
		"{{WORKDIR}}", absDir(workDir),
	).Replace(subagentPrompt)
}
