package notes

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAGYHelper(t *testing.T) {
	mode := os.Getenv("FRAGILE_AGY_HELPER")
	if mode == "" {
		return
	}
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 4096), maxLine)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Println(string(b))
	}

	send(map[string]any{
		"type":       "system",
		"subtype":    "init",
		"session_id": "test-agy-session",
		"model":      "gemini-3.8-flash",
		"provider":   "agy",
	})

	for s.Scan() {
		var v struct {
			Type    string `json:"type"`
			Message struct {
				Content []map[string]any `json:"content"`
			} `json:"message"`
			Request struct {
				Subtype string `json:"subtype"`
			} `json:"request"`
		}
		if err := json.Unmarshal(s.Bytes(), &v); err != nil {
			continue
		}

		if v.Type == "control_request" && v.Request.Subtype == "interrupt" {
			send(map[string]any{
				"type":     "result",
				"subtype":  "error_during_execution",
				"is_error": false,
				"result":   "turn interrupted",
				"provider": "agy",
			})
			continue
		}

		if v.Type == "user" {
			text := ""
			for _, blk := range v.Message.Content {
				if blk["type"] == "text" {
					text = fmt.Sprint(blk["text"])
				}
			}

			if text == "hold" {
				continue
			}

			send(map[string]any{
				"type": "assistant",
				"message": map[string]any{
					"content": []any{
						map[string]any{
							"type": "text",
							"text": "hello from agy: " + text,
						},
					},
				},
			})

			send(map[string]any{
				"type":     "result",
				"subtype":  "success",
				"is_error": false,
				"result":   "hello from agy: " + text,
				"provider": "agy",
			})

			if mode == "clean-exit" {
				os.Exit(0)
			}
		}
	}
	os.Exit(0)
}

func fakeAGY(t *testing.T, mode string) string {
	t.Helper()
	t.Setenv("FRAGILE_AGY_HELPER", mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return writeFake(t, t.TempDir(), "exec "+strconv.Quote(exe)+" -test.run='^TestAGYHelper$'")
}

func agyRunner(t *testing.T, mode string) (*Runner, *Store, Session, <-chan string) {
	r, s, _, _ := newTestRunner(t, "")
	r.AGYCommand = fakeAGY(t, mode)
	r.Interactive = true
	se, err := s.CreateSessionWithProvider("agy", t.TempDir(), ProviderAGY)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 100)
	r.OnLine = func(_ Agent, b []byte) { events <- string(b) }
	t.Cleanup(r.StopAll)
	return r, s, se, events
}

func nextAGY(t *testing.T, ch <-chan string, typ string) string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line := <-ch:
			var v struct {
				Type string `json:"type"`
			}
			json.Unmarshal([]byte(line), &v)
			if v.Type == typ {
				return line
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event type %q", typ)
			return ""
		}
	}
}

func TestAGYProviderPersistence(t *testing.T) {
	_, s, legacy, _ := newTestRunner(t, "")
	if legacy.Provider != ProviderClaude {
		t.Fatalf("unexpected default provider: %+v", legacy)
	}

	se, err := s.CreateSessionWithProvider("agy-test", t.TempDir(), ProviderAGY)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(se.ID)
	if err != nil || got.Provider != ProviderAGY {
		t.Fatalf("got %+v, err: %v", got, err)
	}

	if _, err = s.CreateSessionWithProvider("invalid", "", "unknown-provider"); err == nil {
		t.Fatal("invalid provider should be rejected")
	}
}

func TestAGYModelResolution(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"flash", "gemini-3.8-flash", false},
		{"pro", "gemini-3.8-pro", false},
		{"flash_lite", "gemini-3.8-flash-lite", false},
		{"gemini-3.8-pro", "gemini-3.8-pro", false},
		{"sonnet", "", true},
		{"opus", "", true},
		{"haiku", "", true},
		{"claude-3-7-sonnet", "", true},
		{"invalid with spaces", "", true},
	}

	for _, tc := range cases {
		got, err := resolveAGYModel(tc.input)
		if tc.wantErr && err == nil {
			t.Errorf("resolveAGYModel(%q) expected error, got nil", tc.input)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("resolveAGYModel(%q) unexpected error: %v", tc.input, err)
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("resolveAGYModel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestAGYEnvCleaning(t *testing.T) {
	env := []string{
		"PATH=/bin",
		"USER=test",
		"GEMINI_API_KEY=secret123",
		"GOOGLE_API_KEY=secret456",
		"ANTIGRAVITY_API_KEY=secret789",
		"AGY_API_KEY=secretabc",
		"VERTEX_API_KEY=secretdef",
		"ANTHROPIC_API_KEY=secretclaude",
	}

	cleaned := agyEnv(env)
	for _, kv := range cleaned {
		k, _, _ := strings.Cut(kv, "=")
		if strings.Contains(k, "API_KEY") {
			t.Errorf("agyEnv leaked key %q: %v", k, cleaned)
		}
	}
}

func TestAGYPromptFormatting(t *testing.T) {
	a := Agent{Role: roleOrchestrator}
	base := "# You are the orchestrator\nClaude Code task.\n4. **Spawn**\n5. **Wait loop.**\n"
	prompt := agyPrompt(a, base)
	if strings.Contains(prompt, "Claude Code") {
		t.Errorf("prompt still contains Claude Code: %s", prompt)
	}
	if !strings.Contains(prompt, "Antigravity") {
		t.Errorf("prompt missing Antigravity: %s", prompt)
	}
	if !strings.Contains(prompt, "Fragile MCP server") {
		t.Errorf("prompt missing MCP server note: %s", prompt)
	}
}

func TestAGYRunnerTurnsAndInterrupt(t *testing.T) {
	r, _, se, ch := agyRunner(t, "normal")

	a, err := r.StartOrchestrator(se.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	nextAGY(t, ch, "system")

	// First chat turn
	if err = r.SendUser(a.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	line := nextAGY(t, ch, "assistant")
	if !strings.Contains(line, "hello from agy: hello") {
		t.Fatalf("unexpected response line: %s", line)
	}
	nextAGY(t, ch, "result")

	// Hold and interrupt
	if err = r.SendUser(a.ID, "hold"); err != nil {
		t.Fatal(err)
	}
	if err = r.Interrupt(a.ID); err != nil {
		t.Fatal(err)
	}
	nextAGY(t, ch, "result")
}
