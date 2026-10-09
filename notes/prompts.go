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

	lifecycleInteractive = `Your first message is the task. The user can send more messages later, in this same session (the board and sub-agents stay): treat each as a new instruction or an answer. Ending your turn does not stop running sub-agents: after spawning, answering or acting, end your turn. Fragile wakes you with a message starting "[Fragile]" when a sub-agent posts a ` + "`done`, `blocker` or `question`" + ` note or exits (see Workflow step 5).`

	waitOneShot = `**Wait loop.** Read the board once, then repeat until ` + "`running_subagents`" + ` is 0:
   - Call ` + "`wait_for_notes(since_id=<highest note id seen>, finished_subagents=<value from the last result, 0 at first>)`" + `. It wakes on a new note or a sub-agent exit.
   - Act on anything new (see below). ` + "`get_subagent_status()`" + ` tells you who exited and how.
   - Never wait with shell loops, process checks or file polling; ` + "`wait_for_notes`" + ` is the only way to wait. An empty result is just a timeout: call it again.`

	waitInteractive = `**Wait by ending your turn.** Do not loop on ` + "`wait_for_notes`" + `; every wake-up costs a full turn. Once sub-agents are running and you have nothing to act on, end your turn. Fragile then sends you a message starting "[Fragile] Board update" when a sub-agent posts a ` + "`done`, `blocker` or `question`" + ` note or exits, or when a sub-agent ends its turn without a ` + "`done`" + ` note ("idle, waiting": it waits for a note, e.g. your answer); several events arrive batched in one message. It is automatic, not from the user. Any note you post wakes idle sub-agents.
   - On such a message, read the new notes (` + "`read_notes(since_id=...)`" + `), act on them (see below), then end your turn again. ` + "`get_subagent_status()`" + ` tells you who exited and how.
   - Use ` + "`wait_for_notes`" + ` only for a short wait within a turn (e.g. a reply you expect within a minute). Never wait with shell loops, process checks or file polling.`

	subWaitOneShot = "`wait_for_notes` blocks until a note with id > `since_id` exists (optionally of `type`) or `timeout_s` (default 60, max 120) passes; it returns `{notes}`, empty on timeout. If you need another agent's work, wait for its `done` note this way (`type=\"done\"`, check the author) and call it again on timeout. Never wait with shell loops, process checks or file polling, and do not read files another agent is still writing."

	subWaitInteractive = `**Wait by ending your turn.** To wait for another agent's work or for an answer, end your turn; do not loop on ` + "`wait_for_notes`" + `. Fragile wakes you with a message starting "[Fragile] Board update" (automatic, not from the user) when anyone else posts a note. Then read the new notes (` + "`read_notes(since_id=...)`" + `) and continue, or end your turn again if what you need is not there yet. Never wait with shell loops, process checks or file polling, and do not read files another agent is still writing. Post your ` + "`done`" + ` note only when you are fully finished: after it, your process ends with your turn.`

	subWaitReplyOneShot     = "`wait_for_notes` for the orchestrator's reply"
	subWaitReplyInteractive = "end your turn when only the reply is left: Fragile wakes you when a note is posted"

	escalationOneShot = `Escalation is log-only here and there is no answer: the tool will say so. Then proceed with your best judgement and record the assumption as a ` + "`decision`" + ` note.`

	escalationInteractive = `The call returns at once; the user's answer arrives later as a new user message starting "Answer to your escalation #N". Do not guess the answer and do not poll for it. Meanwhile continue with work that does not depend on the answer; between events, end your turn as usual (Fragile wakes you for sub-agent events). If nothing can proceed without the answer, post a ` + "`decision`" + ` note saying you are waiting on it and end your turn. When the answer arrives, act on it and record it as a ` + "`decision`" + ` note.`
)

// OrchestratorPrompt returns the system prompt appended for the orchestrator,
// with the absolute working directory substituted in. interactive selects the
// desktop-app lifecycle and escalation behaviour over the one-shot CLI's.
func OrchestratorPrompt(workDir string, interactive bool) string {
	life, wait, esc := lifecycleOneShot, waitOneShot, escalationOneShot
	if interactive {
		life, wait, esc = lifecycleInteractive, waitInteractive, escalationInteractive
	}
	return strings.NewReplacer("{{WORKDIR}}", absDir(workDir), "{{LIFECYCLE}}", life, "{{WAIT}}", wait, "{{ESCALATION}}", esc).Replace(orchestratorPrompt)
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
// interactive selects waiting by ending the turn (woken over stdin) over the wait_for_notes loop.
func SubagentPrompt(agentID int64, task, workDir string, interactive bool) string {
	wait, reply := subWaitOneShot, subWaitReplyOneShot
	if interactive {
		wait, reply = subWaitInteractive, subWaitReplyInteractive
	}
	return strings.NewReplacer(
		"{{AGENT_ID}}", strconv.FormatInt(agentID, 10),
		"{{TASK}}", task,
		"{{WORKDIR}}", absDir(workDir),
		"{{WAIT}}", wait,
		"{{WAIT_REPLY}}", reply,
	).Replace(subagentPrompt)
}
