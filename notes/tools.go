package notes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const roleOrchestrator = "orchestrator"

var (
	noteTypes    = []string{"decision", "blocker", "heads_up", "done", "question"}
	noteStatuses = []string{"open", "resolved"}
)

const noteTypeHelp = `Note types: ` +
	`"decision" = agreed on, others should follow it; ` +
	`"blocker" = you are stuck and need something to proceed; ` +
	`"heads_up" = a change you made that others may depend on; ` +
	`"done" = your work is finished, with a summary; ` +
	`"question" = needs an answer from another agent or the orchestrator.`

// CheckNoteType and CheckNoteStatus validate user-supplied note fields the way the tools do.
func CheckNoteType(v string) error   { return checkEnum("type", v, noteTypes) }
func CheckNoteStatus(v string) error { return checkEnum("status", v, noteStatuses) }

const scopeHelp = `Only "session" (the board shared by all agents in this session) is available.`

// newMCPServer builds the tool set for one agent. Orchestrator-only tools are
// not registered for sub-agents.
func (s *Server) newMCPServer(a Agent, cache *mcp.SchemaCache) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "fragile-notes", Version: "0.1.0"}, &mcp.ServerOptions{SchemaCache: cache})
	addTool(srv, a, "read_notes", "Read notes from the shared board, oldest first. "+scopeHelp+" All filters are optional. "+noteTypeHelp, false, s.readNotes)
	addTool(srv, a, "post_note", "Post a note to the shared board; you are recorded as the author. "+scopeHelp+" "+noteTypeHelp, false, s.postNote)
	addTool(srv, a, "update_note", "Change a note's status (open or resolved; any agent may do this) and/or its content (only the note's author may). "+
		"Resolve a question or blocker once it has been answered.", false, s.updateNote)
	addTool(srv, a, "wait_for_notes", "Block until a note with id > since_id (optionally of `type`) is on the shared board, then return it. "+
		"Returns at once if one already exists; returns an empty notes list after timeout_s (default 60, max 120). Use this instead of sleeping or polling. "+
		"Orchestrator: also pass finished_subagents to wake when a sub-agent exits or crashes; the result then carries finished_subagents and running_subagents counts "+
		"(and returns at once when no sub-agent is running).", false, s.waitForNotes)
	addTool(srv, a, "spawn_subagent", "Orchestrator only. Start a new sub-agent process for a self-contained task and create its task record. "+
		"Returns the new agent id and task id.", true, s.spawnSubagent)
	addTool(srv, a, "get_subagent_status", "Orchestrator only. Status of one sub-agent (pass id), or of all sub-agents in the session (omit id): "+
		"agent and task status, pid, times, exit code, its latest note, and whether it posted a done note.", true, s.subagentStatus)
	addTool(srv, a, "escalate_to_user", "Orchestrator only. Raise a product decision you cannot reasonably make yourself to the user. "+
		"It returns at once. In the desktop app the user's answer arrives later as a new user message; in the one-shot CLI it is only logged and no answer comes back.", true, s.escalate)
	return srv
}

// addTool registers fn under name, wrapping its result as text (JSON unless it is a string). A returned
// error becomes a tool error. orchOnly tools are hidden from, and refused for,
// non-orchestrators.
func addTool[In any](srv *mcp.Server, a Agent, name, desc string, orchOnly bool, fn func(context.Context, Agent, In) (any, error)) {
	if orchOnly && a.Role != roleOrchestrator {
		return
	}
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			if orchOnly && a.Role != roleOrchestrator {
				return nil, nil, fmt.Errorf("%s is orchestrator-only", name)
			}
			if rc, ok := ctx.Value(requestCtxKey{}).(context.Context); ok { // end the call when the client goes away
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				defer context.AfterFunc(rc, cancel)()
			}
			out, err := fn(ctx, a, in)
			if err != nil {
				return nil, nil, err
			}
			text, ok := out.(string) // plain messages go as-is, everything else as JSON
			if !ok {
				b, err := json.Marshal(out)
				if err != nil {
					return nil, nil, err
				}
				text = string(b)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
		})
}

func checkScope(scope string) error {
	if scope != "session" {
		return fmt.Errorf("invalid scope %q: only \"session\" is available in this phase", scope)
	}
	return nil
}

func checkEnum(what, v string, valid []string) error {
	if !slices.Contains(valid, v) {
		return fmt.Errorf("invalid %s %q: must be one of %v", what, v, valid)
	}
	return nil
}

// Notes

type readNotesIn struct {
	Scope         string `json:"scope" jsonschema:"must be \"session\""`
	Type          string `json:"type,omitempty" jsonschema:"only notes of this type"`
	Status        string `json:"status,omitempty" jsonschema:"only notes with this status: open or resolved"`
	AuthorAgentID int64  `json:"author_agent_id,omitempty" jsonschema:"only notes written by this agent id"`
	SinceID       int64  `json:"since_id,omitempty" jsonschema:"only notes with an id greater than this (to fetch what is new)"`
}

func (s *Server) readNotes(_ context.Context, a Agent, in readNotesIn) (any, error) {
	if err := checkScope(in.Scope); err != nil {
		return nil, err
	}
	if in.Type != "" {
		if err := checkEnum("type", in.Type, noteTypes); err != nil {
			return nil, err
		}
	}
	if in.Status != "" {
		if err := checkEnum("status", in.Status, noteStatuses); err != nil {
			return nil, err
		}
	}
	board, err := s.Store.SessionBoard(a.SessionID)
	if err != nil {
		return nil, err
	}
	notes, err := s.Store.ListNotes(a.SessionID, board, NoteFilter{Type: in.Type, Status: in.Status, AuthorID: in.AuthorAgentID, SinceID: in.SinceID})
	if notes == nil {
		notes = []Note{}
	}
	return notes, err
}

type postNoteIn struct {
	Scope   string `json:"scope" jsonschema:"must be \"session\""`
	Type    string `json:"type" jsonschema:"decision, blocker, heads_up, done or question"`
	Content string `json:"content" jsonschema:"the note text; short and specific"`
}

func (s *Server) postNote(_ context.Context, a Agent, in postNoteIn) (any, error) {
	if err := checkScope(in.Scope); err != nil {
		return nil, err
	}
	if err := checkEnum("type", in.Type, noteTypes); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Content) == "" {
		return nil, errors.New("content must not be empty")
	}
	board, err := s.Store.SessionBoard(a.SessionID)
	if err != nil {
		return nil, err
	}
	n, err := s.Store.PostNote(a.SessionID, board, a.ID, in.Type, in.Content)
	if err != nil {
		return nil, err
	}
	s.Log.Write(EventNotePosted, a.SessionID, a.ID, n)
	return n, nil
}

type updateNoteIn struct {
	ID      int64   `json:"id" jsonschema:"id of the note"`
	Content *string `json:"content,omitempty" jsonschema:"new text (author only)"`
	Status  *string `json:"status,omitempty" jsonschema:"open or resolved"`
}

func (s *Server) updateNote(_ context.Context, a Agent, in updateNoteIn) (any, error) {
	if in.Content == nil && in.Status == nil {
		return nil, errors.New("provide content and/or status")
	}
	if in.Status != nil {
		if err := checkEnum("status", *in.Status, noteStatuses); err != nil {
			return nil, err
		}
	}
	if in.Content != nil && strings.TrimSpace(*in.Content) == "" {
		return nil, errors.New("content must not be empty")
	}
	old, err := s.Store.GetNote(a.SessionID, in.ID)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("note %d not found", in.ID)
	} else if err != nil {
		return nil, err
	}
	if in.Content != nil && old.AuthorID != a.ID {
		return nil, fmt.Errorf("only the author (agent %d) may change the content of note %d; you may change its status", old.AuthorID, old.ID)
	}
	n, err := s.Store.UpdateNote(a.SessionID, in.ID, in.Content, in.Status)
	if err != nil {
		return nil, err
	}
	s.Log.Write(EventNoteUpdated, a.SessionID, a.ID, n)
	return n, nil
}

// Orchestrator tools

type spawnIn struct {
	Title    string   `json:"title,omitempty" jsonschema:"short title shown on the agent card, at most 60 chars"`
	Task     string   `json:"task" jsonschema:"self-contained task: goal, files owned, constraints, what done looks like"`
	Scopes   []string `json:"scopes,omitempty" jsonschema:"note scopes the sub-agent gets; only [\"session\"] (default)"`
	Model    string   `json:"model,omitempty" jsonschema:"optional model id for this sub-agent provider. Claude accepts sonnet, opus, haiku or a full claude- id; Codex accepts a full Codex model id; AGY accepts flash, pro, flash_lite or an AGY model id. Omit to use the provider default."`
	Provider string   `json:"provider,omitempty" jsonschema:"optional CLI for this sub-agent: claude, codex or agy; must be enabled for the session. Omit for the session's first enabled CLI."`
}

// modelAliases maps the spawn_subagent model aliases to full model ids.
var modelAliases = map[string]string{
	"sonnet": "claude-sonnet-5-5",
	"opus":   "claude-opus-5-5",
	"haiku":  "claude-haiku-4-5-20251001",
}

// resolveModel turns a spawn_subagent model (alias, full "claude-" id or empty) into
// the id passed to --model; empty stays empty (the CLI's default).
func resolveModel(m string) (string, error) {
	m = strings.TrimSpace(m)
	if id, ok := modelAliases[m]; ok {
		return id, nil
	}
	if m == "" || (strings.HasPrefix(m, "claude-") && !strings.ContainsAny(m, " \t\n")) {
		return m, nil
	}
	return "", fmt.Errorf(`invalid model %q: use "sonnet", "opus", "haiku" or a full model id starting with "claude-"`, m)
}

func (s *Server) spawnSubagent(_ context.Context, a Agent, in spawnIn) (any, error) {
	if strings.TrimSpace(in.Task) == "" {
		return nil, errors.New("task must not be empty")
	}
	for _, sc := range in.Scopes { // validated, but only "session" exists, so nothing else to apply
		if err := checkScope(sc); err != nil {
			return nil, err
		}
	}
	if s.Runner == nil {
		return nil, errors.New("sub-agent runner not configured")
	}
	sub, err := s.Runner.SpawnSubagent(a.SessionID, a.ID, in.Title, in.Task, strings.TrimSpace(in.Model), strings.TrimSpace(in.Provider))
	if err != nil {
		return nil, err
	}
	return map[string]int64{"agent_id": sub.ID, "task_id": sub.TaskID}, nil
}

type statusIn struct {
	ID int64 `json:"id,omitempty" jsonschema:"sub-agent id; omit to list all sub-agents in the session"`
}

type subagentStatus struct {
	AgentID     int64  `json:"agent_id"`
	TaskTitle   string `json:"task_title"`
	AgentStatus string `json:"agent_status"`
	TaskStatus  string `json:"task_status"`
	PID         int    `json:"pid,omitempty"`
	CreatedAt   string `json:"created_at"`
	ExitedAt    string `json:"exited_at,omitempty"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	LatestNote  *Note  `json:"latest_note"`
	HasDoneNote bool   `json:"has_done_note"`
}

func (s *Server) subagentStatus(_ context.Context, caller Agent, in statusIn) (any, error) {
	var agents []Agent
	if in.ID != 0 {
		a, err := s.Store.GetAgent(caller.SessionID, in.ID)
		if errors.Is(err, ErrNotFound) || err == nil && a.Role != "subagent" {
			return nil, fmt.Errorf("sub-agent %d not found", in.ID)
		} else if err != nil {
			return nil, err
		}
		agents = []Agent{a}
	} else {
		var err error
		if agents, err = s.Store.ListAgents(caller.SessionID, "subagent"); err != nil {
			return nil, err
		}
	}
	board, err := s.Store.SessionBoard(caller.SessionID)
	if err != nil {
		return nil, err
	}
	out := []subagentStatus{}
	for _, a := range agents {
		st := subagentStatus{AgentID: a.ID, AgentStatus: a.Status, PID: a.PID, CreatedAt: a.CreatedAt, ExitedAt: a.ExitedAt, ExitCode: a.ExitCode}
		if t, err := s.Store.GetTask(caller.SessionID, a.TaskID); err == nil {
			st.TaskTitle, st.TaskStatus = t.Title, t.Status
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		notes, err := s.Store.ListNotes(caller.SessionID, board, NoteFilter{AuthorID: a.ID})
		if err != nil {
			return nil, err
		}
		if len(notes) > 0 {
			st.LatestNote = &notes[len(notes)-1]
		}
		for _, n := range notes {
			st.HasDoneNote = st.HasDoneNote || n.Type == "done"
		}
		out = append(out, st)
	}
	if in.ID != 0 {
		return out[0], nil
	}
	return out, nil
}

type escalateIn struct {
	Question string `json:"question" jsonschema:"the decision the user needs to make"`
	Context  string `json:"context" jsonschema:"background and the options you see"`
}

func (s *Server) escalate(_ context.Context, a Agent, in escalateIn) (any, error) {
	if strings.TrimSpace(in.Question) == "" {
		return nil, errors.New("question must not be empty")
	}
	e, err := s.Store.CreateEscalation(a.SessionID, a.ID, in.Question, in.Context)
	if err != nil {
		return nil, err
	}
	s.Log.Write(EventEscalation, a.SessionID, a.ID, e)
	if s.OnEscalation != nil {
		s.OnEscalation(a, e)
		return "Escalated to the user. Their answer will arrive as a new user message; continue with work that doesn't depend on it, or end your turn.", nil
	}
	fmt.Printf("\n=== ESCALATION from agent %d (#%d) ===\nQuestion: %s\nContext: %s\n=== END ESCALATION ===\n\n", a.ID, e.ID, printable(e.Question), printable(e.Context))
	return "Logged for the user; no answer is available in this phase. Proceed with your best judgement and record the assumption as a `decision` note.", nil
}

// printable drops control characters except newline and tab, so agent-supplied text
// cannot inject terminal escape sequences.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}
