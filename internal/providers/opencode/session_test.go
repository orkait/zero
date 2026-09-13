package opencode

import "testing"

func TestWithSessionHeaderGeneratesStableID(t *testing.T) {
	first := WithSessionHeader(nil)[SessionHeaderName]
	second := WithSessionHeader(map[string]string{"X-Trace": "test"})
	if first == "" {
		t.Fatal("WithSessionHeader() generated an empty session id")
	}
	if second[SessionHeaderName] != first {
		t.Fatalf("session id = %q, want the process id %q on every call", second[SessionHeaderName], first)
	}
	if second["X-Trace"] != "test" {
		t.Fatalf("X-Trace = %q, want unrelated headers preserved", second["X-Trace"])
	}
}

func TestWithSessionHeaderKeepsConfiguredID(t *testing.T) {
	headers := WithSessionHeader(map[string]string{"X-OpenCode-Session": "ses_configured"})
	if len(headers) != 1 {
		t.Fatalf("headers = %#v, want one session entry and no case-variant duplicate", headers)
	}
	if headers[SessionHeaderName] != "ses_configured" {
		t.Fatalf("session id = %q, want the configured value regardless of header casing", headers[SessionHeaderName])
	}
}

func TestWithSessionHeaderReplacesEmptyConfiguredID(t *testing.T) {
	headers := WithSessionHeader(map[string]string{SessionHeaderName: "  "})
	if headers[SessionHeaderName] != SessionID() {
		t.Fatalf("session id = %q, want the generated id when the configured one is blank", headers[SessionHeaderName])
	}
}
