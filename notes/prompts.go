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

// Mode-specific parts of the orchestrator prompt. One-shot is the CLI (cmd/fragile);
// interactive is the desktop app, where the user chats with the orchestrator.
const (
	lifecycleOneShot = `You are one-shot: when you stop, the session ends. Never finish while any sub-agent is still running.`

	lifecycleInteractive = `Your first message is the task. The user can send more messages later, in this same session (the board and sub-agents stay): treat each as a new instruction or an answer. Ending your turn only means you are waiting for the user. Never end your turn while any sub-agent is still running - nothing wakes you when one finishes - except while waiting for an escalation answer (see Escalation).`

	escalationOneShot = `Escalation is log-only here and there is no answer: the tool will say so. Then proceed with your best judgement and record the assumption as a ` + "`decision`" + ` note.`

	escalationInteractive = `The call returns at once; the user's answer arrives later as a new user message starting "Answer to your escalation #N". Do not guess the answer and do not poll for it. Meanwhile continue with work that does not depend on the answer, including the usual wait loop for running sub-agents. If nothing can proceed without the answer, post a ` + "`decision`" + ` note saying you are waiting on it and end your turn. When the answer arrives, act on it and record it as a ` + "`decision`" + ` note.`
)

// OrchestratorPrompt returns the system prompt appended for the orchestrator,
// with the absolute working directory substituted in. interactive selects the
// desktop-app lifecycle and escalation behaviour over the one-shot CLI's.
func OrchestratorPrompt(workDir string, interactive bool) string {
	life, esc := lifecycleOneShot, escalationOneShot
	if interactive {
		life, esc = lifecycleInteractive, escalationInteractive
	}
	return strings.NewReplacer("{{WORKDIR}}", absDir(workDir), "{{LIFECYCLE}}", life, "{{ESCALATION}}", esc).Replace(orchestratorPrompt)
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
