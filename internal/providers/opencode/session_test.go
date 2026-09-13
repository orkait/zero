package opencode

import "testing"

func TestSessionIDFallsBackToAStableProcessID(t *testing.T) {
	t.Cleanup(func() { boundSession.Store(nil) })
	boundSession.Store(nil)

	first := SessionID()
	if first == "" {
		t.Fatal("SessionID() returned empty with no bound session")
	}
	if second := SessionID(); second != first {
		t.Fatalf("SessionID() = %q then %q, want one id per process", first, second)
	}
}

func TestBindSessionIsStablePerZeroSessionAndDistinctAcrossThem(t *testing.T) {
	t.Cleanup(func() { boundSession.Store(nil) })

	BindSession("ses_01JABC")
	first := SessionID()
	BindSession("ses_01JABC")
	if again := SessionID(); again != first {
		t.Fatalf("SessionID() = %q, want %q — a resumed session must keep its routing id", again, first)
	}
	BindSession("ses_01JXYZ")
	if other := SessionID(); other == first {
		t.Fatalf("SessionID() = %q for a different Zero session, want a distinct routing id", other)
	}
	// The Zero session id is internal: only a digest of it goes upstream.
	if first == "ses_01JABC" {
		t.Fatal("SessionID() leaked the Zero session id verbatim")
	}
}

func TestBindSessionIgnoresBlankIDs(t *testing.T) {
	t.Cleanup(func() { boundSession.Store(nil) })

	BindSession("ses_01JABC")
	bound := SessionID()
	BindSession("   ")
	if got := SessionID(); got != bound {
		t.Fatalf("SessionID() = %q after a blank bind, want the previous id %q", got, bound)
	}
}

func TestWithSessionHeaderLeavesResolutionToRequestTime(t *testing.T) {
	headers := WithSessionHeader(map[string]string{"X-Trace": "test"})
	if value, ok := headers[SessionHeaderName]; !ok || value != "" {
		t.Fatalf("%s = %q (present=%v), want an empty value resolved per request", SessionHeaderName, value, ok)
	}
	if headers["X-Trace"] != "test" {
		t.Fatalf("X-Trace = %q, want unrelated headers preserved", headers["X-Trace"])
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
