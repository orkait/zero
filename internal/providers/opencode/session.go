package opencode

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Gitlawb/zero/internal/providers/providerio"
)

// SessionHeaderName is the routing header the OpenCode Go gateway requires on
// every model call. Without it both the OpenAI- and Anthropic-compatible
// endpoints answer 400 MissingSessionID.
const SessionHeaderName = "x-opencode-session"

var (
	boundSession atomic.Pointer[string]
	fallbackOnce sync.Once
	fallbackID   string
)

func init() {
	// Provider clients are built at startup, before the run's Zero session
	// exists, so the header value is resolved per request rather than captured
	// in the client's header map.
	providerio.RegisterHeaderValueResolver(SessionHeaderName, SessionID)
}

// BindSession ties this process's OpenCode Go routing id to the run's Zero
// session, so one conversation keeps one id across restarts and resumes while
// separate conversations stay separately routed. The id sent upstream is a
// digest, not the Zero session id itself.
func BindSession(zeroSessionID string) {
	zeroSessionID = strings.TrimSpace(zeroSessionID)
	if zeroSessionID == "" {
		return
	}
	digest := sha256.Sum256([]byte("zero/opencode-go/" + zeroSessionID))
	id := "ses_" + hex.EncodeToString(digest[:16])
	boundSession.Store(&id)
}

// WithSessionHeader returns headers carrying the OpenCode Go session id. A
// value already present in headers (a user-configured customHeaders entry) is
// kept; otherwise the header is left empty for per-request resolution.
func WithSessionHeader(headers map[string]string) map[string]string {
	resolved := make(map[string]string, len(headers)+1)
	configured := ""
	for key, value := range headers {
		if strings.EqualFold(strings.TrimSpace(key), SessionHeaderName) {
			if strings.TrimSpace(value) != "" {
				configured = value
			}
			continue
		}
		resolved[key] = value
	}
	resolved[SessionHeaderName] = configured
	return resolved
}

// SessionID returns the bound session id, or a per-process id when no Zero
// session has been bound (provider health probes, one-shot model calls).
func SessionID() string {
	if bound := boundSession.Load(); bound != nil {
		return *bound
	}
	fallbackOnce.Do(func() {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			fallbackID = "ses_zero"
			return
		}
		fallbackID = "ses_" + hex.EncodeToString(buf)
	})
	return fallbackID
}
