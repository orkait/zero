package cline

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gitlawb/zero/internal/config"
)

func TestMatchesCatalogAndGatewayHost(t *testing.T) {
	tests := []struct {
		name    string
		profile config.ProviderProfile
		want    bool
	}{
		{
			name:    "catalog id",
			profile: config.ProviderProfile{CatalogID: "cline"},
			want:    true,
		},
		{
			name:    "catalog alias",
			profile: config.ProviderProfile{CatalogID: "cline-pass"},
			want:    true,
		},
		{
			name:    "gateway host",
			profile: config.ProviderProfile{BaseURL: "https://api.cline.bot/api/v1"},
			want:    true,
		},
		{
			name:    "other openai compatible host",
			profile: config.ProviderProfile{CatalogID: "groq", BaseURL: "https://api.groq.com/openai/v1"},
			want:    false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Matches(test.profile); got != test.want {
				t.Fatalf("Matches() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestEnsureModelPrefix(t *testing.T) {
	if got := EnsureModelPrefix("glm-5.2", DefaultModelType); got != "cline-pass/glm-5.2" {
		t.Fatalf("bare model = %q", got)
	}
	if got := EnsureModelPrefix("cline-pass/glm-5.2", DefaultModelType); got != "cline-pass/glm-5.2" {
		t.Fatalf("already prefixed = %q", got)
	}
	if got := EnsureModelPrefix("deepseek/deepseek-v4-flash", DefaultModelType); got != "deepseek/deepseek-v4-flash" {
		t.Fatalf("foreign prefix rewritten = %q", got)
	}
}

func TestResolveBearerUsesFreshStoredToken(t *testing.T) {
	token := jwtWithClaims(t, time.Now().Add(time.Hour).Unix(), "client-1")
	path := writeProvidersFile(t, providersFile{
		LastUsedProvider: DefaultModelType,
		Providers: map[string]providerEntry{
			DefaultModelType: {Settings: providerSettings{Auth: Auth{
				AccessToken:  "workos:" + token,
				RefreshToken: "refresh-1",
			}}},
		},
	})

	header, value, ok, err := Resolver(Options{ConfigPath: path, Now: time.Now})(context.Background(), false)
	if err != nil || !ok {
		t.Fatalf("Resolve() = ok=%v err=%v", ok, err)
	}
	if header != "Authorization" || value != "Bearer workos:"+token {
		t.Fatalf("auth = (%q, %q)", header, value)
	}
}

func TestResolveBearerRefreshesNearExpiryAndWritesBack(t *testing.T) {
	oldToken := jwtWithClaims(t, time.Now().Add(time.Minute).Unix(), "client-1")
	newToken := jwtWithClaims(t, time.Now().Add(time.Hour).Unix(), "client-1")
	path := writeProvidersFile(t, providersFile{
		LastUsedProvider: DefaultModelType,
		Providers: map[string]providerEntry{
			DefaultModelType: {Settings: providerSettings{Auth: Auth{
				AccessToken:  oldToken,
				RefreshToken: "refresh-old",
			}}},
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user_management/authenticate" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		form := string(body)
		if !strings.Contains(form, "grant_type=refresh_token") || !strings.Contains(form, "refresh_token=refresh-old") || !strings.Contains(form, "client_id=client-1") {
			t.Fatalf("refresh form = %q", form)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{
			"access_token":  newToken,
			"refresh_token": "refresh-new",
		}); err != nil {
			t.Fatal(err)
		}
	}))
	t.Cleanup(server.Close)

	header, value, ok, err := Resolver(Options{
		ConfigPath: path,
		Now:        time.Now,
		HTTPClient: server.Client(),
		TokenURL:   server.URL + "/user_management/authenticate",
	})(context.Background(), false)
	if err != nil || !ok {
		t.Fatalf("Resolve() = ok=%v err=%v", ok, err)
	}
	if value != "Bearer workos:"+newToken {
		t.Fatalf("auth value = %q", value)
	}
	if header != "Authorization" {
		t.Fatalf("header = %q", header)
	}

	persisted := readProvidersFile(t, path)
	got := persisted.Providers[DefaultModelType].Settings.Auth.AccessToken
	if got != "workos:"+newToken {
		t.Fatalf("written access token = %q", got)
	}
	if persisted.Providers[DefaultModelType].Settings.Auth.RefreshToken != "refresh-new" {
		t.Fatalf("written refresh token = %q", persisted.Providers[DefaultModelType].Settings.Auth.RefreshToken)
	}
}

func TestResolveBearerForceRefreshOn401(t *testing.T) {
	fresh := jwtWithClaims(t, time.Now().Add(time.Hour).Unix(), "client-1")
	rotated := jwtWithClaims(t, time.Now().Add(2*time.Hour).Unix(), "client-1")
	path := writeProvidersFile(t, providersFile{
		LastUsedProvider: DefaultModelType,
		Providers: map[string]providerEntry{
			DefaultModelType: {Settings: providerSettings{Auth: Auth{
				AccessToken:  fresh,
				RefreshToken: "refresh-old",
			}}},
		},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{
			"access_token":  rotated,
			"refresh_token": "refresh-new",
		}); err != nil {
			t.Fatal(err)
		}
	}))
	t.Cleanup(server.Close)

	_, value, ok, err := Resolver(Options{
		ConfigPath: path,
		Now:        time.Now,
		HTTPClient: server.Client(),
		TokenURL:   server.URL + "/user_management/authenticate",
	})(context.Background(), true)
	if err != nil || !ok {
		t.Fatalf("Resolve() = ok=%v err=%v", ok, err)
	}
	if value != "Bearer workos:"+rotated {
		t.Fatalf("forced refresh value = %q", value)
	}
}

func TestResolveBearerMissingConfig(t *testing.T) {
	_, _, ok, err := Resolver(Options{ConfigPath: filepath.Join(t.TempDir(), "missing.json")})(context.Background(), false)
	if ok || err == nil {
		t.Fatal("expected error for missing Cline config")
	}
	if !strings.Contains(err.Error(), "Sign in to Cline") && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestConfigPathPrefersCLINE_CONFIG_PATH(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	t.Setenv("CLINE_CONFIG_PATH", path)
	if got := ConfigPath(Options{}); got != path {
		t.Fatalf("ConfigPath() = %q, want %q", got, path)
	}
}

func TestHasSession(t *testing.T) {
	t.Setenv("CLINE_CONFIG_PATH", filepath.Join(t.TempDir(), "missing.json"))
	if HasSession(Options{}) {
		t.Fatal("missing Cline config must not count as a session")
	}

	path := writeProvidersFile(t, providersFile{
		LastUsedProvider: DefaultModelType,
		Providers: map[string]providerEntry{
			DefaultModelType: {Settings: providerSettings{Auth: Auth{
				AccessToken:  "workos:access",
				RefreshToken: "refresh",
			}}},
		},
	})
	if !HasSession(Options{ConfigPath: path}) {
		t.Fatal("Cline providers.json with tokens must count as a session")
	}

	refreshOnly := writeProvidersFile(t, providersFile{
		LastUsedProvider: DefaultModelType,
		Providers: map[string]providerEntry{
			DefaultModelType: {Settings: providerSettings{Auth: Auth{
				RefreshToken: "refresh-only",
			}}},
		},
	})
	if !HasSession(Options{ConfigPath: refreshOnly}) {
		t.Fatal("refresh token alone must still count as a session")
	}

	empty := writeProvidersFile(t, providersFile{
		LastUsedProvider: DefaultModelType,
		Providers:        map[string]providerEntry{DefaultModelType: {}},
	})
	if HasSession(Options{ConfigPath: empty}) {
		t.Fatal("empty Cline auth must not count as a session")
	}
}

func jwtWithClaims(t *testing.T, exp int64, clientID string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]any{"exp": exp, "client_id": clientID})
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func writeProvidersFile(t *testing.T, file providersFile) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "providers.json")
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readProvidersFile(t *testing.T, path string) providersFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file providersFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file
}
