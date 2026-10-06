package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseLine(t *testing.T) {
	longResult := strings.Repeat("x", maxResultContent+100)
	longLine, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user",
		"content": []map[string]any{{"type": "tool_result", "tool_use_id": "t9", "content": longResult}}}})

	for _, tc := range []struct {
		name string
		line string
		want []parsedEvent // payloads compared as JSON
	}{
		{"system init kept raw",
			`{"type":"system","subtype":"init","cwd":"/w","session_id":"abc","tools":["Bash","Read"]}`,
			[]parsedEvent{{"system", `{"type":"system","subtype":"init","cwd":"/w","session_id":"abc","tools":["Bash","Read"]}`}}},
		{"assistant text",
			`{"type":"assistant","message":{"id":"msg_1","role":"assistant","content":[{"type":"text","text":"Planning the work."}],"usage":{"input_tokens":3}},"session_id":"abc"}`,
			[]parsedEvent{{"assistant_text", `{"text":"Planning the work."}`}}},
		{"text and tool_use in one message",
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Reading it."},{"type":"tool_use","id":"toolu_01","name":"Read","input":{"file_path":"/w/a.go"}}]}}`,
			[]parsedEvent{{"assistant_text", `{"text":"Reading it."}`}, {"tool_use", `{"id":"toolu_01","name":"Read","input":{"file_path":"/w/a.go"}}`}}},
		{"mcp tool_use",
			`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_02","name":"mcp__fragile__spawn_subagent","input":{"task":"write hello.txt"}}]}}`,
			[]parsedEvent{{"tool_use", `{"id":"toolu_02","name":"mcp__fragile__spawn_subagent","input":{"task":"write hello.txt"}}`}}},
		{"thinking only gives nothing",
			`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hmm","signature":"s"}]}}`, nil},
		{"blank text gives nothing",
			`{"type":"assistant","message":{"content":[{"type":"text","text":"  \n"}]}}`, nil},
		{"tool_result string",
			`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01","type":"tool_result","content":"package main\n"}]}}`,
			[]parsedEvent{{"tool_result", `{"tool_use_id":"toolu_01","content":"package main\n","is_error":false}`}}},
		{"tool_result block list with error",
			`{"type":"user","message":{"content":[{"tool_use_id":"toolu_03","type":"tool_result","is_error":true,"content":[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]}]}}`,
			[]parsedEvent{{"tool_result", `{"tool_use_id":"toolu_03","content":"line1\nline2","is_error":true}`}}},
		{"user message with plain string content",
			`{"type":"user","message":{"role":"user","content":"hello"}}`, nil},
		{"result kept raw",
			`{"type":"result","subtype":"success","is_error":false,"duration_ms":1200,"result":"Done.","total_cost_usd":0.012}`,
			[]parsedEvent{{"result", `{"type":"result","subtype":"success","is_error":false,"duration_ms":1200,"result":"Done.","total_cost_usd":0.012}`}}},
		{"other types ignored", `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed"}}`, nil},
		{"not json", `Error: something on stderr`, nil},
		{"empty", ``, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLine([]byte(tc.line))
			if len(got) != len(tc.want) {
				t.Fatalf("got %d events %+v, want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				var g, w any
				if got[i].Type != tc.want[i].Type || json.Unmarshal([]byte(got[i].Payload), &g) != nil ||
					json.Unmarshal([]byte(tc.want[i].Payload), &w) != nil || !reflect.DeepEqual(g, w) {
					t.Errorf("event %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}

	got := parseLine(longLine)
	var p struct{ Content string }
	if len(got) != 1 || json.Unmarshal([]byte(got[0].Payload), &p) != nil ||
		len(p.Content) > maxResultContent+200 || !strings.Contains(p.Content, "truncated") {
		t.Errorf("long tool_result not truncated: %d events", len(got))
	}
}

func TestToolSummary(t *testing.T) {
	for in, want := range map[string]string{
		`{"file_path":"/w/a.go","limit":5}`:              "/w/a.go",
		`{"command":"ls  -la\n  /tmp"}`:                  "ls -la /tmp",
		`{"task":"write hello.txt"}`:                     "write hello.txt",
		`{"since_id":3}`:                                 `{"since_id":3}`,
		`{"command":"` + strings.Repeat("a", 200) + `"}`: strings.Repeat("a", 120) + "...",
	} {
		if got := toolSummary(json.RawMessage(in)); got != want {
			t.Errorf("toolSummary(%.40s) = %q, want %q", in, got, want)
		}
	}
}
