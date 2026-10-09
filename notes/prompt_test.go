package notes

import (
	"strings"
	"testing"
)

func TestSessionPromptTextEscalationThreshold(t *testing.T) {
	se := Session{
		SessionConfig: SessionConfig{
			EnabledProviders:    []string{ProviderClaude},
			EscalationThreshold: "only when blocked",
		},
	}
	got := sessionPromptText(se)
	wantParagraph := "**When to escalate to the user** (overrides the Escalation section): only when blocked"
	if !strings.Contains(got, wantParagraph) {
		t.Fatalf("sessionPromptText with threshold missing expected paragraph %q, got:\n%s", wantParagraph, got)
	}

	// When threshold is empty: absent from prompt text
	se.EscalationThreshold = ""
	got = sessionPromptText(se)
	if strings.Contains(got, "**When to escalate to the user**") {
		t.Fatalf("sessionPromptText with empty threshold should not contain escalation paragraph, got:\n%s", got)
	}

	// When threshold is whitespace only: absent from prompt text
	se.EscalationThreshold = "   \n\t  "
	got = sessionPromptText(se)
	if strings.Contains(got, "**When to escalate to the user**") {
		t.Fatalf("sessionPromptText with whitespace threshold should not contain escalation paragraph, got:\n%s", got)
	}
}
