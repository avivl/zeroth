package memory

import (
	"strings"
	"testing"
)

func TestForSessionKeepsOperatorSessionAndAgent(t *testing.T) {
	t.Parallel()
	b := NewBook()
	if _, err := b.Write(Human("alice"), KindOperator, "", "style.tests", "prefer table tests", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write(Human("alice"), KindSession, "sess-a", "this.run", "only a", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write(Human("alice"), KindSession, "sess-b", "other.run", "only b", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write(Human("alice"), KindAgent, "a_default", "voice", "terse", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write(Human("alice"), KindAgent, "a_other", "voice", "chatty", "operator"); err != nil {
		t.Fatal(err)
	}

	got := ForSession(b.Slice(), "sess-a", "a_default")
	if len(got) != 3 {
		t.Fatalf("slice %+v", got)
	}
	keys := make([]string, 0, len(got))
	for _, f := range got {
		keys = append(keys, f.Kind+"/"+f.RefID+"/"+f.Key)
	}
	want := []string{
		"agent/a_default/voice",
		"operator//style.tests",
		"session/sess-a/this.run",
	}
	for i, w := range want {
		if keys[i] != w {
			t.Fatalf("keys %v, want %v", keys, want)
		}
	}
	compiled := Compile(got)
	for _, wantBody := range []string{"prefer table tests", "only a", "terse"} {
		if !strings.Contains(compiled, wantBody) {
			t.Fatalf("compiled missing %q: %s", wantBody, compiled)
		}
	}
	if strings.Contains(compiled, "only b") || strings.Contains(compiled, "chatty") {
		t.Fatalf("compiled leaked other session/agent: %s", compiled)
	}
}

func TestParseProposalTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		target, sess, agent string
		kind, ref, key      string
	}{
		{"session/style", "s1", "a1", KindSession, "s1", "style"},
		{"operator/kernel.rule", "s1", "a1", KindOperator, "", "kernel.rule"},
		{"agent/voice", "s1", "a1", KindAgent, "a1", "voice"},
		{"bare-key", "s1", "a1", KindSession, "s1", "bare-key"},
		{"./session/style", "s1", "a1", KindSession, "s1", "style"},
		{"session/", "s1", "a1", KindSession, "s1", "session/"},
		{"unknown/x", "s1", "a1", KindSession, "s1", "unknown/x"},
	}
	for _, tc := range tests {
		kind, ref, key := ParseProposalTarget(tc.target, tc.sess, tc.agent)
		if kind != tc.kind || ref != tc.ref || key != tc.key {
			t.Fatalf("%q: got %s %q %q, want %s %q %q",
				tc.target, kind, ref, key, tc.kind, tc.ref, tc.key)
		}
	}
}
