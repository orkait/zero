package cline

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gitlawb/zero/internal/config"
	"github.com/Gitlawb/zero/internal/fsutil"
	"github.com/Gitlawb/zero/internal/providercatalog"
	"github.com/Gitlawb/zero/internal/providers/providerio"
)

const (
	ID               = "cline"
	APIBaseURL       = "https://api.cline.bot/api/v1"
	DefaultModelType = "cline-pass"
	DefaultModel     = "cline-pass/glm-5.2"
	workosPrefix     = "workos:"
	refreshBuffer    = 5 * time.Minute
	refreshTimeout   = 30 * time.Second
	defaultTokenURL  = "https://api.workos.com/user_management/authenticate"
)

// Options configures Cline token resolution. Empty fields use process defaults.
type Options struct {
	ConfigPath string
	ProviderID string
	ModelType  string
	TokenURL   string
	Now        func() time.Time
	HTTPClient *http.Client
	Getenv     func(string) string
	Home       string
}

type Auth struct {
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"`
	AccountID    string `json:"accountId,omitempty"`
}

type providerSettings struct {
	Provider string `json:"provider,omitempty"`
	Auth     Auth   `json:"auth,omitempty"`
	Model    string `json:"model,omitempty"`
}

type providerEntry struct {
	Settings  providerSettings `json:"settings,omitempty"`
	UpdatedAt string           `json:"updatedAt,omitempty"`
}

type providersFile struct {
	Version          int                      `json:"version,omitempty"`
	LastUsedProvider string                   `json:"lastUsedProvider,omitempty"`
	Providers        map[string]providerEntry `json:"providers,omitempty"`
}

// Matches reports whether this profile should authenticate against Cline's
// gateway using the WorkOS session stored by the Cline app.
func Matches(profile config.ProviderProfile) bool {
	id := providercatalog.NormalizeID(profile.CatalogID)
	if id == ID || id == DefaultModelType {
		return true
	}
	return isClineBaseURL(profile.BaseURL)
}

// HasSession reports whether the Cline app has stored a usable WorkOS session
// (access token and/or refresh token) that Zero can present to api.cline.bot.
func HasSession(options Options) bool {
	file, err := readProviders(ConfigPath(options))
	if err != nil {
		return false
	}
	entry, ok := file.Providers[selectProviderID(file, options)]
	if !ok {
		return false
	}
	return strings.TrimSpace(entry.Settings.Auth.AccessToken) != "" ||
		strings.TrimSpace(entry.Settings.Auth.RefreshToken) != ""
}

func isClineBaseURL(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "api.cline.bot")
}

// EnsureModelPrefix adds the Cline model-type namespace to a bare model id.
// An id that already contains "/" is left unchanged so a free-tier model such
// as deepseek/deepseek-v4-flash is not rewritten onto cline-pass.
func EnsureModelPrefix(model string, modelType string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return model
	}
	if strings.Contains(model, "/") {
		return model
	}
	modelType = strings.TrimSpace(modelType)
	if modelType == "" {
		modelType = DefaultModelType
	}
	return modelType + "/" + model
}

// ConfigPath returns the Cline providers.json path.
func ConfigPath(options Options) string {
	if path := strings.TrimSpace(options.ConfigPath); path != "" {
		return path
	}
	getenv := options.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if path := strings.TrimSpace(getenv("CLINE_CONFIG_PATH")); path != "" {
		return path
	}
	home := strings.TrimSpace(options.Home)
	if dir := strings.TrimSpace(getenv("CLINE_DIR")); dir != "" {
		home = dir
	} else if home == "" {
		if userHome, err := os.UserHomeDir(); err == nil {
			home = userHome
		}
	}
	return filepath.Join(home, ".cline", "data", "settings", "providers.json")
}

// Resolver returns a TokenResolver that yields Authorization: Bearer workos:<jwt>.
func Resolver(options Options) providerio.TokenResolver {
	return func(ctx context.Context, forceRefresh bool) (string, string, bool, error) {
		token, err := resolveBearer(ctx, options, forceRefresh)
		if err != nil {
			return "", "", false, err
		}
		return "Authorization", "Bearer " + token, true, nil
	}
}

func resolveBearer(ctx context.Context, options Options, forceRefresh bool) (string, error) {
	path := ConfigPath(options)
	file, err := readProviders(path)
	if err != nil {
		return "", err
	}
	providerID := selectProviderID(file, options)
	entry, ok := file.Providers[providerID]
	if !ok || strings.TrimSpace(entry.Settings.Auth.AccessToken) == "" {
		return "", fmt.Errorf("no Cline credentials for provider %q in %s. Sign in to Cline first", providerID, path)
	}
	auth := entry.Settings.Auth
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	expiry := tokenExpiry(auth)
	if !forceRefresh && (expiry.IsZero() || expiry.Sub(now()) > refreshBuffer) {
		return addWorkosPrefix(auth.AccessToken), nil
	}
	refreshed, err := refreshWorkosToken(ctx, options, auth)
	if err != nil {
		return "", err
	}
	writeBackAuth(path, file, providerID, refreshed)
	return addWorkosPrefix(refreshed.AccessToken), nil
}

func selectProviderID(file providersFile, options Options) string {
	if id := strings.TrimSpace(options.ProviderID); id != "" {
		return id
	}
	getenv := options.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if id := strings.TrimSpace(getenv("CLINE_PROVIDER_ID")); id != "" {
		return id
	}
	if id := strings.TrimSpace(file.LastUsedProvider); id != "" {
		return id
	}
	return DefaultModelType
}

func readProviders(path string) (providersFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return providersFile{}, fmt.Errorf("Cline config not found at %s. Sign in to Cline first, or set CLINE_CONFIG_PATH", path)
		}
		return providersFile{}, fmt.Errorf("read Cline config: %w", err)
	}
	var file providersFile
	if err := json.Unmarshal(data, &file); err != nil {
		return providersFile{}, fmt.Errorf("Cline config at %s is not valid JSON", path)
	}
	if file.Providers == nil {
		file.Providers = map[string]providerEntry{}
	}
	return file, nil
}

func refreshWorkosToken(ctx context.Context, options Options, auth Auth) (Auth, error) {
	if strings.TrimSpace(auth.RefreshToken) == "" {
		return Auth{}, fmt.Errorf("Cline token expired and no refresh token is stored. Sign in to Cline first")
	}
	clientID, _ := jwtClaim(auth.AccessToken, "client_id").(string)
	if strings.TrimSpace(clientID) == "" {
		return Auth{}, fmt.Errorf("Cline token is missing a client_id claim; cannot refresh. Sign in to Cline first")
	}
	tokenURL := strings.TrimSpace(options.TokenURL)
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {auth.RefreshToken},
		"client_id":     {clientID},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Auth{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: refreshTimeout}
	}
	response, err := client.Do(request)
	if err != nil {
		return Auth{}, fmt.Errorf("Cline token refresh failed: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Auth{}, fmt.Errorf("Cline token refresh failed (%d). Sign in to Cline first", response.StatusCode)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.AccessToken == "" || payload.RefreshToken == "" {
		return Auth{}, fmt.Errorf("Cline token refresh returned an invalid response")
	}
	refreshed := Auth{
		AccessToken:  addWorkosPrefix(payload.AccessToken),
		RefreshToken: payload.RefreshToken,
		AccountID:    auth.AccountID,
	}
	if expiry := tokenExpiry(Auth{AccessToken: payload.AccessToken}); !expiry.IsZero() {
		refreshed.ExpiresAt = expiry.UnixMilli()
	}
	return refreshed, nil
}

func writeBackAuth(path string, file providersFile, providerID string, refreshed Auth) {
	entry, ok := file.Providers[providerID]
	if !ok {
		return
	}
	entry.Settings.Auth.AccessToken = refreshed.AccessToken
	entry.Settings.Auth.RefreshToken = refreshed.RefreshToken
	entry.Settings.Auth.ExpiresAt = refreshed.ExpiresAt
	if refreshed.AccountID != "" {
		entry.Settings.Auth.AccountID = refreshed.AccountID
	}
	entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	file.Providers[providerID] = entry
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".cline-providers-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		cleanup()
		return
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return
	}
	if err := fsutil.RenameWithRetry(tmpName, path, nil); err != nil {
		cleanup()
	}
}

func tokenExpiry(auth Auth) time.Time {
	if exp, ok := jwtClaim(auth.AccessToken, "exp").(float64); ok && exp > 0 {
		return time.Unix(int64(exp), 0)
	}
	if auth.ExpiresAt > 0 {
		// Cline stores either epoch ms or seconds; treat large values as ms.
		if auth.ExpiresAt > 1_000_000_000_000 {
			return time.UnixMilli(auth.ExpiresAt)
		}
		return time.Unix(auth.ExpiresAt, 0)
	}
	return time.Time{}
}

func jwtClaim(token string, key string) any {
	raw := stripWorkosPrefix(token)
	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		padded := parts[1]
		if rem := len(padded) % 4; rem != 0 {
			padded += strings.Repeat("=", 4-rem)
		}
		payload, err = base64.URLEncoding.DecodeString(padded)
		if err != nil {
			return nil
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims[key]
}

func stripWorkosPrefix(token string) string {
	trimmed := strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(trimmed), workosPrefix) {
		return trimmed[len(workosPrefix):]
	}
	return trimmed
}

func addWorkosPrefix(token string) string {
	trimmed := strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(trimmed), workosPrefix) {
		return trimmed
	}
	return workosPrefix + trimmed
}
