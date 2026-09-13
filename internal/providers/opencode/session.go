package opencode

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
)

// SessionHeaderName is the routing header the OpenCode Go gateway requires on
// every model call. Without it both the OpenAI- and Anthropic-compatible
// endpoints answer 400 MissingSessionID; the value only has to stay stable for
// the caller's conversation so the gateway can keep its cache affinity.
const SessionHeaderName = "x-opencode-session"

var (
	sessionOnce sync.Once
	sessionID   string
)

// WithSessionHeader returns headers carrying this process's OpenCode Go session
// id. An id already present in headers (a user-configured customHeaders entry)
// is kept.
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
	if configured != "" {
		resolved[SessionHeaderName] = configured
		return resolved
	}
	resolved[SessionHeaderName] = SessionID()
	return resolved
}

// SessionID returns the per-process OpenCode Go session id, generating it once.
func SessionID() string {
	sessionOnce.Do(func() {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			sessionID = "ses_zero"
			return
		}
		sessionID = "ses_" + hex.EncodeToString(buf)
	})
	return sessionID
}
