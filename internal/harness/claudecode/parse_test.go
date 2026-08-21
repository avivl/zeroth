package claudecode

import (
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/harness"
)

func TestExtractorTokensToolCallsEffects(t *testing.T) {
	t.Parallel()
	ex := &extractor{}
	var got []harness.Event
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"sess-9"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"README.md"}}]}}`,
		`{"type":"result","result":"{\"effects\":[{\"op\":\"modify\",\"target\":\"README.md\",\"diff\":\"+x\"}]}","is_error":false}`,
	}
	for _, line := range lines {
		got = append(got, ex.handleLine(line)...)
	}
	if ex.session != "sess-9" {
		t.Fatalf("session = %q", ex.session)
	}
	if len(got) != 3 {
		t.Fatalf("events = %#v", got)
	}
	if got[0].Kind != harness.EventToken || got[0].Payload != "hello" {
		t.Fatalf("token = %#v", got[0])
	}
	if got[1].Kind != harness.EventToolCall || !strings.Contains(got[1].Payload, "Read") {
		t.Fatalf("tool = %#v", got[1])
	}
	if got[2].Kind != harness.EventEffects || len(got[2].Effects) != 1 || got[2].Effects[0].Path != "README.md" {
		t.Fatalf("effects = %#v", got[2])
	}
}

func TestExtractorRedactsAPIKey(t *testing.T) {
	t.Parallel()
	key := "sk-ant-secret-key-value"
	ex := &extractor{key: key}
	got := ex.handleLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"leak ` + key + `"}}}`)
	if len(got) != 1 {
		t.Fatalf("events = %#v", got)
	}
	if strings.Contains(got[0].Payload, key) {
		t.Fatalf("key leaked in payload: %q", got[0].Payload)
	}
	if !strings.Contains(got[0].Payload, "[redacted]") {
		t.Fatalf("payload = %q", got[0].Payload)
	}
}

func TestExtractorNonJSONIsToken(t *testing.T) {
	t.Parallel()
	ex := &extractor{}
	got := ex.handleLine("plain text token")
	if len(got) != 1 || got[0].Kind != harness.EventToken || got[0].Payload != "plain text token" {
		t.Fatalf("got %#v", got)
	}
}
