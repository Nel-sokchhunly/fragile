package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// BenchmarkSessionSnapshot measures the backend portion of reopening a session.
// It does not include Wails transport, rendering or provider/network latency.
// Fixtures are synthetic and setup is excluded. Run multiple samples with:
// go test ./app -run '^$' -bench '^BenchmarkSessionSnapshot$' -benchmem -count=10
func BenchmarkSessionSnapshot(b *testing.B) {
	for _, size := range []struct {
		name                      string
		messages, escalationEvery int
	}{
		{"ordinary_200", 200, 0},
		{"escalations_200", 200, 5},
		{"escalations_1000", 1000, 5},
	} {
		b.Run(size.name, func(b *testing.B) {
			s, err := notes.OpenStore(filepath.Join(b.TempDir(), "fixture.db"))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { s.Close() })
			se, err := s.CreateSession("benchmark")
			if err != nil {
				b.Fatal(err)
			}
			ag, err := s.CreateAgent(se.ID, "orchestrator", 0, 0)
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < size.messages; i++ {
				typ, payload := evAssistantText, `{"text":"`+strings.Repeat("synthetic text ", 16)+`"}`
				if size.escalationEvery > 0 && i%size.escalationEvery == 0 {
					e, err := s.CreateEscalation(se.ID, ag.ID, fmt.Sprintf("Question %d", i), "synthetic context")
					if err != nil {
						b.Fatal(err)
					}
					if i%2 == 0 {
						if _, err := s.AnswerEscalation(se.ID, e.ID, "synthetic answer"); err != nil {
							b.Fatal(err)
						}
					}
					typ, payload = evEscalation, fmt.Sprintf(`{"escalation_id":%d}`, e.ID)
				}
				if _, err := s.AppendAgentEvent(se.ID, ag.ID, typ, payload); err != nil {
					b.Fatal(err)
				}
			}
			a := &App{store: s}
			// One explicitly excluded warmup validates the complete fixture.
			snap, err := a.GetSession(se.ID)
			if err != nil || len(snap.Chat) != size.messages {
				b.Fatalf("fixture: chat=%d err=%v", len(snap.Chat), err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				snap, err := a.GetSession(se.ID)
				if err != nil || len(snap.Chat) != size.messages {
					b.Fatalf("snapshot: chat=%d err=%v", len(snap.Chat), err)
				}
			}
		})
	}
}
