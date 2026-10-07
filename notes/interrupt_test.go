package notes

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// Interrupt writes one stream-json control_request line to the orchestrator's
// stdin, with a fresh request id each time, and leaves the process running.
func TestRunnerInterrupt(t *testing.T) {
	r, _, sess, _ := newTestRunner(t, `while IFS= read -r line; do echo "$line"; done`)
	r.Interactive = true
	var mu sync.Mutex
	var lines []string
	r.OnLine = func(a Agent, l []byte) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, string(l))
	}
	if err := r.Interrupt(12345); err != ErrNotRunning {
		t.Fatalf("unknown agent: err = %v", err)
	}
	a, err := r.StartOrchestrator(sess.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer r.StopAll()
	for range 2 {
		if err := r.Interrupt(a.ID); err != nil {
			t.Fatal(err)
		}
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		mu.Lock()
		n := len(lines)
		mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lines = %q", lines)
		}
	}
	ids := map[string]bool{}
	for _, l := range lines {
		var m struct {
			Type      string            `json:"type"`
			RequestID string            `json:"request_id"`
			Request   map[string]string `json:"request"`
		}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line %q: %v", l, err)
		}
		if m.Type != "control_request" || m.Request["subtype"] != "interrupt" || len(m.Request) != 1 || !strings.HasPrefix(m.RequestID, "fragile-interrupt-") {
			t.Fatalf("line = %s", l)
		}
		ids[m.RequestID] = true
	}
	if len(ids) != 2 {
		t.Errorf("request ids not unique: %q", lines)
	}
	if !r.Running(a.ID) {
		t.Error("interrupt ended the process")
	}
	r.StopAll()
	if err := r.Interrupt(a.ID); err != ErrNotRunning {
		t.Errorf("stopped agent: err = %v", err)
	}
}
