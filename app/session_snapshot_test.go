package main

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Nel-sokchhunly/fragile/notes"
)

// Reopening a session must preserve answered questions in history, exclude
// missing/foreign references, and reflect answers made since the last read.
func TestSessionSnapshotEscalationHistory(t *testing.T) {
	store, err := notes.OpenStore(filepath.Join(t.TempDir(), "fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := &App{store: store}
	session, err := store.CreateSession("local")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateSession("foreign")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.CreateAgent(session.ID, "orchestrator", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	foreignAgent, err := store.CreateAgent(other.ID, "orchestrator", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	open, err := store.CreateEscalation(session.ID, agent.ID, "Open question", "context")
	if err != nil {
		t.Fatal(err)
	}
	answered, err := store.CreateEscalation(session.ID, agent.ID, "Answered question", "context")
	if err != nil {
		t.Fatal(err)
	}
	answered, err = store.AnswerEscalation(session.ID, answered.ID, "saved answer")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := store.CreateEscalation(other.ID, foreignAgent.ID, "Private question", "private")
	if err != nil {
		t.Fatal(err)
	}
	var expected []ChatItem
	for _, e := range []notes.Escalation{open, answered, open, foreign, {ID: 999999}} {
		ev, err := store.AppendAgentEvent(session.ID, agent.ID, evEscalation, fmt.Sprintf(`{"escalation_id":%d}`, e.ID))
		if err != nil {
			t.Fatal(err)
		}
		item, ok := a.chatItem(session.ID, ev)
		if ok {
			expected = append(expected, item)
		}
	}
	if _, err := store.AppendAgentEvent(session.ID, agent.ID, evEscalation, `not json`); err != nil {
		t.Fatal(err)
	}
	if len(expected) != 3 {
		t.Fatalf("expected valid history: %+v", expected)
	}
	snapshot, err := a.GetSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.Chat, expected) {
		t.Fatalf("history changed: got %+v want %+v", snapshot.Chat, expected)
	}
	if !reflect.DeepEqual(snapshot.Escalations, []notes.Escalation{open}) {
		t.Fatalf("open badge: %+v", snapshot.Escalations)
	}
	if snapshot.Chat[0].Escalation == snapshot.Chat[2].Escalation {
		t.Fatal("duplicate rows share a mutable pointer")
	}
	if _, err := store.AnswerEscalation(session.ID, open.ID, "new answer"); err != nil {
		t.Fatal(err)
	}
	next, err := a.GetSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Escalations) != 0 || len(next.Chat) != 3 {
		t.Fatalf("after answer: %+v", next)
	}
	for _, i := range []int{0, 2} {
		if next.Chat[i].Escalation.Answer != "new answer" || snapshot.Chat[i].Escalation.Status != "open" {
			t.Fatalf("freshness/old snapshot changed at %d", i)
		}
	}
	if _, err := a.GetSession(999999); err != notes.ErrNotFound {
		t.Fatalf("missing session: %v", err)
	}
}
