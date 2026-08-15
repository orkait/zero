package providers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Gitlawb/zero/internal/config"
	"github.com/Gitlawb/zero/internal/oauth"
)

func TestOAuthLoginForProfileBindsBearerAndAccountToSameLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-tokens.json")
	t.Setenv("ZERO_OAUTH_STORAGE", "file")
	t.Setenv("ZERO_OAUTH_TOKENS_PATH", path)
	store, err := oauth.NewStore(oauth.StoreOptions{FilePath: path})
	if err != nil {
		t.Fatalf("oauth store: %v", err)
	}
	key := oauth.ProviderKey("chatgpt")
	if err := store.Save(key, oauth.Token{
		AccessToken: "subscription-token",
		Account:     "account-42",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("save OAuth token: %v", err)
	}

	resolver, loginKey := OAuthLoginForProfile(config.ProviderProfile{Name: "codex", CatalogID: "chatgpt"})
	if resolver == nil || loginKey != key {
		t.Fatalf("OAuth login = (%v, %q), want resolver bound to %q", resolver != nil, loginKey, key)
	}
	header, value, ok, err := resolver(context.Background(), false)
	if err != nil || !ok || header != "Authorization" || value != "Bearer subscription-token" {
		t.Fatalf("bearer resolution = (%q, %q, %v, %v)", header, value, ok, err)
	}
	account, ok, err := CodexAccountResolverForLogin(loginKey)(context.Background())
	if err != nil || !ok || account != "account-42" {
		t.Fatalf("account resolution = (%q, %v, %v)", account, ok, err)
	}
}

func TestOAuthLoginForProfileReturnsNoResolverWithoutUsableLogin(t *testing.T) {
	t.Setenv("ZERO_OAUTH_STORAGE", "file")
	t.Setenv("ZERO_OAUTH_TOKENS_PATH", filepath.Join(t.TempDir(), "oauth-tokens.json"))

	for _, test := range []struct {
		name    string
		profile config.ProviderProfile
	}{
		{
			name:    "profile with its own credential has no OAuth candidates",
			profile: config.ProviderProfile{Name: "openai", CatalogID: "openai", APIKey: "sk-configured"},
		},
		{
			name:    "candidate has no stored login",
			profile: config.ProviderProfile{Name: "chatgpt", CatalogID: "chatgpt"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver, key := OAuthLoginForProfile(test.profile)
			if resolver != nil || key != "" {
				t.Fatalf("OAuthLoginForProfile() = (%v, %q), want (nil, empty)", resolver != nil, key)
			}
		})
	}
}

func TestOAuthLoginForProfileUsesClineAmbientResolver(t *testing.T) {
	t.Setenv("ZERO_OAUTH_STORAGE", "file")
	t.Setenv("ZERO_OAUTH_TOKENS_PATH", filepath.Join(t.TempDir(), "oauth-tokens.json"))

	token := "workos:cline-access"
	path := filepath.Join(t.TempDir(), "providers.json")
	payload := []byte(`{"lastUsedProvider":"cline-pass","providers":{"cline-pass":{"settings":{"auth":{"accessToken":"` + token + `"}}}}}` + "\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLINE_CONFIG_PATH", path)

	resolver, loginKey := OAuthLoginForProfile(config.ProviderProfile{Name: "cline", CatalogID: "cline"})
	if resolver == nil {
		t.Fatal("OAuthLoginForProfile() resolver = nil, want Cline ambient resolver")
	}
	if loginKey != "" {
		t.Fatalf("loginKey = %q, want empty (Cline does not use Zero's OAuth store)", loginKey)
	}
	header, value, ok, err := resolver(context.Background(), false)
	if err != nil || !ok || header != "Authorization" || value != "Bearer "+token {
		t.Fatalf("Cline bearer = (%q, %q, %v, %v)", header, value, ok, err)
	}
}
