package notes

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentEventTailIsolationAndOrder(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "tail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.CreateSession("first")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateSession("other")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.CreateAgent(first.ID, "subagent", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	interleaved, err := s.CreateAgent(first.ID, "subagent", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var want []AgentEvent
	for i := 0; i < 2007; i++ {
		e, err := s.AppendAgentEvent(first.ID, agent.ID, "assistant_text", fmt.Sprintf("%d", i))
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, e)
		if _, err := s.AppendAgentEvent(first.ID, interleaved.ID, "assistant_text", "different"); err != nil {
			t.Fatal(err)
		}
	}
	for _, limit := range []int{1, 17, 2000, 0, -1, 5000} {
		count := limit
		if count <= 0 || count > 2000 {
			count = 2000
		}
		got, err := s.ListAgentEventTail(first.ID, agent.ID, limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != count {
			t.Fatalf("limit %d got %d", limit, len(got))
		}
		for i, e := range got {
			if e != want[len(want)-count+i] {
				t.Fatalf("limit %d event %d mismatch", limit, i)
			}
		}
	}
	if got, err := s.ListAgentEventTail(other.ID, agent.ID, 2000); err != nil || len(got) != 0 {
		t.Fatalf("foreign session: %v %v", got, err)
	}
	if err := s.DeleteAgentEvent(first.ID, agent.ID, want[len(want)-1].ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListAgentEventTail(first.ID, agent.ID, 1)
	if err != nil || len(got) != 1 || got[0] != want[len(want)-2] {
		t.Fatalf("deleted newest: %v %v", got, err)
	}
	empty, err := s.CreateAgent(first.ID, "subagent", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListAgentEventTail(first.ID, empty.ID, 2000); err != nil || len(got) != 0 {
		t.Fatalf("empty: %v %v", got, err)
	}
	// The indexed reverse scan must not introduce a full table scan or sort.
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT `+agentEventCols+` FROM agent_events WHERE agent_id = ? AND agent_id IN (SELECT id FROM agent_instances WHERE session_id = ?) ORDER BY id DESC LIMIT ?`, agent.ID, first.ID, 2000)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	indexed := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "agent_events_agent") {
			indexed = true
		}
		if strings.Contains(detail, "TEMP B-TREE") {
			t.Fatalf("unexpected sort: %s", detail)
		}
	}
	if !indexed {
		t.Fatal("tail query did not use existing agent_events_agent index")
	}
}

// BenchmarkAgentOutputRetrieval measures the Store portion of the unchanged old
// paged loader versus the bounded tail loader. It excludes Wails, JS and rendering.
func BenchmarkAgentOutputRetrieval(b *testing.B) {
	for _, size := range []int{20, 2000, 10000, 100000} {
		b.Run(fmt.Sprintf("rows%d", size), func(b *testing.B) {
			s, err := OpenStore(filepath.Join(b.TempDir(), "bench.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			sess, _ := s.CreateSession("bench")
			agent, _ := s.CreateAgent(sess.ID, "subagent", 0, 0)
			tx, err := s.db.Begin()
			if err != nil {
				b.Fatal(err)
			}
			stmt, err := tx.Prepare(`INSERT INTO agent_events (agent_id, event_type, payload) VALUES (?, ?, ?)`)
			if err != nil {
				b.Fatal(err)
			}
			payload := `{"text":"` + strings.Repeat("output ", 20) + `"}`
			for i := 0; i < size; i++ {
				if _, err := stmt.Exec(agent.ID, "assistant_text", payload); err != nil {
					b.Fatal(err)
				}
			}
			stmt.Close()
			if err := tx.Commit(); err != nil {
				b.Fatal(err)
			}
			for _, mode := range []string{"paged", "tail"} {
				b.Run(mode, func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					calls, transferred := 0, 0
					for n := 0; n < b.N; n++ {
						var hist []AgentEvent
						if mode == "paged" {
							var since int64
							for {
								page, err := s.ListAgentEvents(sess.ID, agent.ID, since, 1000)
								if err != nil {
									b.Fatal(err)
								}
								calls++
								transferred += len(page)
								hist = append(hist, page...)
								if len(page) < 1000 {
									break
								}
								since = page[len(page)-1].ID
							}
							if len(hist) > 2000 {
								hist = hist[len(hist)-2000:]
							}
						} else {
							var err error
							hist, err = s.ListAgentEventTail(sess.ID, agent.ID, 2000)
							if err != nil {
								b.Fatal(err)
							}
							calls++
							transferred += len(hist)
						}
						want := size
						if want > 2000 {
							want = 2000
						}
						if len(hist) != want {
							b.Fatal("wrong output size")
						}
					}
					b.ReportMetric(float64(calls)/float64(b.N), "queries/op")
					b.ReportMetric(float64(transferred)/float64(b.N), "rows/op")
				})
			}
		})
	}
}
