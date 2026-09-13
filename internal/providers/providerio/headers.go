package providerio

import (
	"net/http"
	"strings"
	"sync"
)

// headerValueResolvers holds the providers that compute a custom header's value
// per request instead of at client construction. A provider registers one when
// its header depends on state that is only known after the client exists (the
// OpenCode Go routing session id is bound once the run's Zero session starts).
var headerValueResolvers sync.Map // canonical header name -> func() string

// RegisterHeaderValueResolver makes resolve the source for the named custom
// header whenever a request carries it with an empty value. Registration is
// process-wide, so it belongs in a provider package's init.
func RegisterHeaderValueResolver(name string, resolve func() string) {
	name = strings.TrimSpace(name)
	if name == "" || resolve == nil {
		return
	}
	headerValueResolvers.Store(http.CanonicalHeaderKey(name), resolve)
}

func resolveHeaderValue(name string) string {
	resolve, ok := headerValueResolvers.Load(http.CanonicalHeaderKey(name))
	if !ok {
		return ""
	}
	return strings.TrimSpace(resolve.(func() string)())
}

type AuthHeaders struct {
	APIKey            string
	DefaultAuthHeader string
	DefaultAuthScheme string
	AuthHeader        string
	AuthScheme        string
	AuthHeaderValue   string
	CustomHeaders     map[string]string
}

func ApplyAuthHeaders(request *http.Request, options AuthHeaders) {
	for key, value := range options.CustomHeaders {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			// An empty configured value carries no information: prefer the
			// registered resolver, and send nothing when there is none rather
			// than an empty header the gateway would have to interpret.
			if value = resolveHeaderValue(key); value == "" {
				continue
			}
		}
		request.Header.Set(key, value)
	}

	header := strings.TrimSpace(options.AuthHeader)
	customHeader := header != ""
	if header == "" {
		header = strings.TrimSpace(options.DefaultAuthHeader)
	}
	if header == "" {
		return
	}

	value := strings.TrimSpace(options.AuthHeaderValue)
	if value == "" {
		apiKey := strings.TrimSpace(options.APIKey)
		if apiKey == "" {
			return
		}
		scheme := strings.TrimSpace(options.AuthScheme)
		if !customHeader && scheme == "" {
			scheme = strings.TrimSpace(options.DefaultAuthScheme)
		}
		if scheme != "" && !strings.EqualFold(scheme, "none") && !strings.EqualFold(scheme, "raw") {
			value = scheme + " " + apiKey
		} else {
			value = apiKey
		}
	}
	request.Header.Set(header, value)
}

func CopyHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	copied := make(map[string]string, len(headers))
	for key, value := range headers {
		copied[key] = value
	}
	return copied
}
