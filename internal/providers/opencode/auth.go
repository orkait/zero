package opencode

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Gitlawb/zero/internal/config"
	"github.com/Gitlawb/zero/internal/providercatalog"
	"github.com/Gitlawb/zero/internal/providers/providerio"
)

const (
	ID          = "opencode-go"
	IDAnthropic = "opencode-go-anthropic-compatible"
	AuthFileKey = "opencode-go"
	apiType     = "api"
)

// Options configures OpenCode Go key resolution. Empty fields use process defaults.
type Options struct {
	AuthPath     string
	Home         string
	Getenv       func(string) string
	APIKeyHeader bool
}

type authEntry struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

// Matches reports whether this profile should authenticate against OpenCode Go
// using the static API key OpenCode stores in auth.json.
func Matches(profile config.ProviderProfile) bool {
	id := providercatalog.NormalizeID(profile.CatalogID)
	if id == ID || id == IDAnthropic {
		return true
	}
	return isOpenCodeGoBaseURL(profile.BaseURL)
}

// UsesAPIKeyHeader reports whether this profile talks to the Anthropic-compatible
// OpenCode Go endpoint, which expects x-api-key instead of Authorization Bearer.
func UsesAPIKeyHeader(profile config.ProviderProfile) bool {
	if providercatalog.NormalizeID(profile.CatalogID) == IDAnthropic {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(profile.BaseURL))
	if err != nil || parsed.Host == "" {
		return false
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	return strings.EqualFold(path, "/zen/go")
}

// HasKey reports whether OPENCODE_API_KEY or OpenCode's auth.json has a usable
// opencode-go API key.
func HasKey(options Options) bool {
	_, ok := resolveKey(options)
	return ok
}

// AuthPath returns the OpenCode auth.json path.
func AuthPath(options Options) string {
	if path := strings.TrimSpace(options.AuthPath); path != "" {
		return path
	}
	getenv := options.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if path := strings.TrimSpace(getenv("OPENCODE_AUTH_PATH")); path != "" {
		return path
	}
	if dir := strings.TrimSpace(getenv("OPENCODE_DIR")); dir != "" {
		return filepath.Join(dir, "auth.json")
	}
	if xdg := strings.TrimSpace(getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Join(xdg, "opencode", "auth.json")
	}
	home := strings.TrimSpace(options.Home)
	if home == "" {
		if userHome, err := os.UserHomeDir(); err == nil {
			home = userHome
		}
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

// Resolver returns a TokenResolver that yields the OpenCode Go API key.
func Resolver(options Options) providerio.TokenResolver {
	return func(context.Context, bool) (string, string, bool, error) {
		key, ok := resolveKey(options)
		if !ok {
			return "", "", false, nil
		}
		if options.APIKeyHeader {
			return "x-api-key", key, true, nil
		}
		return "Authorization", "Bearer " + key, true, nil
	}
}

func resolveKey(options Options) (string, bool) {
	getenv := options.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if key := strings.TrimSpace(getenv("OPENCODE_API_KEY")); key != "" {
		return key, true
	}
	data, err := os.ReadFile(AuthPath(options))
	if err != nil {
		return "", false
	}
	var file map[string]authEntry
	if err := json.Unmarshal(data, &file); err != nil {
		return "", false
	}
	entry, ok := file[AuthFileKey]
	if !ok || !strings.EqualFold(strings.TrimSpace(entry.Type), apiType) {
		return "", false
	}
	key := strings.TrimSpace(entry.Key)
	if key == "" {
		return "", false
	}
	return key, true
}

func isOpenCodeGoBaseURL(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" {
		return false
	}
	if !strings.EqualFold(parsed.Hostname(), "opencode.ai") {
		return false
	}
	path := strings.ToLower(parsed.Path)
	return strings.Contains(path, "/zen/go")
}
