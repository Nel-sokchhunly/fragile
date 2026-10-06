package main

import (
	"encoding/json"
	"strings"
)

// Agent event types stored in agent_events (what the UI renders). Anything else
// Claude Code prints is dropped, except system and result lines which are kept raw.
const (
	evAssistantText = "assistant_text" // {text}
	evToolUse       = "tool_use"       // {id, name, input}
	evToolResult    = "tool_result"    // {tool_use_id, content, is_error}
	evResult        = "result"         // the raw result line (turn finished; carries cost)
	evSystem        = "system"         // the raw system line (init, ...)
	evUserMessage   = "user_message"   // {text}: a chat message from the user, written by the app
	evEscalation    = "escalation"     // {escalation_id}: the orchestrator escalated (orchestrator only)
)

// maxResultContent bounds one stored tool result; the raw agent log keeps everything.
const maxResultContent = 64 << 10

type parsedEvent struct{ Type, Payload string }

// parseLine maps one line of Claude Code's stream-json output to the events it
// holds: one per text or tool_use block of an assistant message, one per
// tool_result block of a user message, and the raw line for system and result.
// Lines that are not JSON, or of other types, give nothing.
func parseLine(line []byte) []parsedEvent {
	var l struct {
		Type    string `json:"type"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &l) != nil {
		return nil
	}
	switch l.Type {
	case "system", "result":
		return []parsedEvent{{l.Type, string(line)}}
	case "assistant", "user":
	default:
		return nil
	}
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
		IsError   bool            `json:"is_error"`
	}
	if json.Unmarshal(l.Message.Content, &blocks) != nil { // content may be a plain string: nothing to show
		return nil
	}
	var out []parsedEvent
	add := func(typ string, payload map[string]any) {
		b, _ := json.Marshal(payload)
		out = append(out, parsedEvent{typ, string(b)})
	}
	for _, b := range blocks {
		switch {
		case l.Type == "assistant" && b.Type == "text" && strings.TrimSpace(b.Text) != "":
			add(evAssistantText, map[string]any{"text": b.Text})
		case l.Type == "assistant" && b.Type == "tool_use":
			if len(b.Input) == 0 {
				b.Input = json.RawMessage("{}")
			}
			add(evToolUse, map[string]any{"id": b.ID, "name": b.Name, "input": b.Input})
		case l.Type == "user" && b.Type == "tool_result":
			add(evToolResult, map[string]any{"tool_use_id": b.ToolUseID, "content": resultText(b.Content), "is_error": b.IsError})
		}
	}
	return out
}

// resultText flattens a tool_result's content (a string, or a list of text
// blocks) to text, cut to maxResultContent.
func resultText(raw json.RawMessage) string {
	var text string
	var s string
	var parts []struct{ Type, Text string }
	switch {
	case json.Unmarshal(raw, &s) == nil:
		text = s
	case json.Unmarshal(raw, &parts) == nil:
		var texts []string
		for _, p := range parts {
			if p.Type == "text" {
				texts = append(texts, p.Text)
			}
		}
		text = strings.Join(texts, "\n")
	default:
		text = string(raw)
	}
	if len(text) > maxResultContent {
		text = strings.ToValidUTF8(text[:maxResultContent], "") + "\n... [truncated; full output in the agent's log file]"
	}
	return text
}
