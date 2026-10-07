package main

import "testing"

func TestParseContext(t *testing.T) {
	for _, tc := range []struct {
		name, line, model string
		want              ctxInfo
	}{
		{"usage sums input and cache tokens", `{"type":"assistant","message":{"content":[],"usage":{"input_tokens":2,"cache_creation_input_tokens":542,"cache_read_input_tokens":28182,"output_tokens":383}}}`, "",
			ctxInfo{Used: 28726}},
		{"init 1m suffix", `{"type":"system","subtype":"init","model":"claude-opus-5-5[1m]"}`, "", ctxInfo{Model: "claude-opus-5-5[1m]", Window: 1_000_000}},
		{"init default window", `{"type":"system","subtype":"init","model":"claude-haiku-4-5"}`, "", ctxInfo{Model: "claude-haiku-4-5", Window: 200_000}},
		{"other system line", `{"type":"system","subtype":"hook"}`, "", ctxInfo{}},
		{"result window of the main model", `{"type":"result","modelUsage":{"claude-haiku-4-5":{"contextWindow":200000},"claude-opus-5-5":{"contextWindow":1000000}}}`, "claude-opus-5-5[1m]",
			ctxInfo{Window: 1_000_000}},
		{"result without the main model", `{"type":"result","modelUsage":{"other":{"contextWindow":123}}}`, "claude-opus-5-5", ctxInfo{}},
		{"not json", `oops`, "", ctxInfo{}},
	} {
		if got := parseContext([]byte(tc.line), tc.model); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestParseRateLimit(t *testing.T) {
	r, ok := parseRateLimit([]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":1791322200,"rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"utilization":0.08,"resetsAt":1791322200},"seven_day":{"utilization":0.01,"resetsAt":1791903600}}}}`))
	if !ok || r.FiveHour == nil || *r.FiveHour != (LimitWindow{0.08, 1791322200}) || r.SevenDay == nil || *r.SevenDay != (LimitWindow{0.01, 1791903600}) {
		t.Fatalf("got %+v, %v", r, ok)
	}
	for _, line := range []string{`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed"}}`, `{"type":"result"}`, `x`} {
		if _, ok := parseRateLimit([]byte(line)); ok {
			t.Errorf("%s parsed", line)
		}
	}
}

// Context and limits are stored: they survive a restart and the limit event fires only on change.
func TestUsageTrackedAndPersisted(t *testing.T) {
	dir := t.TempDir()
	a, ev := newTestApp(t, dir, echoOrchestrator)
	se := startSession(t, a, "hi")
	waitFor(t, "reply", func() bool { return hasChat(a, se.ID, "echo: ") })
	orch := snapshot(t, a, se.ID).Agents[0]
	for _, l := range []string{
		`{"type":"system","subtype":"init","model":"m[1m]"}`,
		`{"type":"assistant","message":{"usage":{"input_tokens":1,"cache_creation_input_tokens":2,"cache_read_input_tokens":3}}}`,
		`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"five_hour":{"utilization":0.5,"resetsAt":10}}}}`,
		`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"five_hour":{"utilization":0.5,"resetsAt":10}}}}`,
	} {
		a.trackUsage(orch, []byte(l))
	}
	got := snapshot(t, a, se.ID).Agents[0]
	if got.ContextUsed != 6 || got.ContextWindow != 1_000_000 {
		t.Fatalf("agent context = %d/%d", got.ContextUsed, got.ContextWindow)
	}
	if got.Model != "m[1m]" {
		t.Fatalf("agent model = %q", got.Model)
	}
	// A model change alone (same context) is stored too.
	a.trackUsage(orch, []byte(`{"type":"system","subtype":"init","model":"claude-opus-5-5[1m]"}`))
	if m := snapshot(t, a, se.ID).Agents[0].Model; m != "claude-opus-5-5[1m]" {
		t.Fatalf("agent model after second init = %q", m)
	}
	if l := a.GetRateLimit(); l == nil || l.FiveHour == nil || l.FiveHour.Utilization != 0.5 || l.SevenDay != nil {
		t.Fatalf("limit = %+v", l)
	}
	waitFor(t, "limit event", func() bool { return len(ev.named(eventRateLimit)) == 1 })
	a.close()

	b, _ := newTestApp(t, dir, "")
	if s := snapshot(t, b, se.ID).Agents[0]; s.ContextUsed != 6 || s.ContextWindow != 1_000_000 || s.Model != "claude-opus-5-5[1m]" {
		t.Errorf("after restart: %d/%d %q", s.ContextUsed, s.ContextWindow, s.Model)
	}
	if l := b.GetRateLimit(); l == nil || l.FiveHour == nil || l.FiveHour.ResetsAt != 10 {
		t.Errorf("limit after restart = %+v", l)
	}
}
