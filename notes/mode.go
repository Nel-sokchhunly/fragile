package notes

import "fmt"

// Session modes. Orchestra is the full team: the main agent gets the Fragile MCP
// tools and the orchestrator prompt. Normal is a one-on-one chat with a single
// agent that has neither; a normal session can be switched to orchestra.
const (
	ModeOrchestra = "orchestra"
	ModeNormal    = "normal"
)

// CheckMode errors for a mode that is neither ModeOrchestra nor ModeNormal.
func CheckMode(mode string) error {
	if mode != ModeOrchestra && mode != ModeNormal {
		return fmt.Errorf("unknown session mode %q (want %q or %q)", mode, ModeOrchestra, ModeNormal)
	}
	return nil
}

// normalOrchestrator reports whether a is the main agent of a normal-mode
// session: it launches without the Fragile MCP server and without a system prompt.
func (r *Runner) normalOrchestrator(a Agent) bool {
	if a.Role != roleOrchestrator || r.store == nil {
		return false
	}
	se, err := r.store.GetSession(a.SessionID)
	return err == nil && se.Mode == ModeNormal
}

// normalAllowedTools is what a normal-mode Claude agent may use without asking:
// the ordinary coding tools, no Fragile tools.
const normalAllowedTools = "Read,Glob,Grep,Bash,Edit,Write,WebFetch,WebSearch"
