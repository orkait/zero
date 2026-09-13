package providerio

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplyAuthHeadersResolvesEmptyCustomHeaderValues(t *testing.T) {
	RegisterHeaderValueResolver("X-Test-Dynamic", func() string { return "resolved" })

	request := httptest.NewRequest(http.MethodPost, "https://example.test/v1/chat", nil)
	ApplyAuthHeaders(request, AuthHeaders{
		CustomHeaders: map[string]string{
			"x-test-dynamic":    "",
			"X-Test-Configured": "kept",
			"X-Test-Unresolved": "",
		},
	})

	if got := request.Header.Get("X-Test-Dynamic"); got != "resolved" {
		t.Fatalf("X-Test-Dynamic = %q, want the registered resolver's value", got)
	}
	if got := request.Header.Get("X-Test-Configured"); got != "kept" {
		t.Fatalf("X-Test-Configured = %q, want the configured value untouched", got)
	}
	// Nothing resolves this one, so it must not go out as an empty header.
	if _, ok := request.Header["X-Test-Unresolved"]; ok {
		t.Fatalf("X-Test-Unresolved = %q, want the header omitted", request.Header.Get("X-Test-Unresolved"))
	}
}
