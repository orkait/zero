package opencode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Gitlawb/zero/internal/config"
)

func TestMatchesCatalogAndGateway(t *testing.T) {
	tests := []struct {
		name    string
		profile config.ProviderProfile
		want    bool
	}{
		{
			name:    "catalog id",
			profile: config.ProviderProfile{CatalogID: "opencode-go"},
			want:    true,
		},
		{
			name:    "alias",
			profile: config.ProviderProfile{CatalogID: "opencode go"},
			want:    true,
		},
		{
			name:    "anthropic-compatible catalog id shares the same key",
			profile: config.ProviderProfile{CatalogID: "opencode-go-anthropic-compatible"},
			want:    true,
		},
		{
			name:    "go gateway host",
			profile: config.ProviderProfile{BaseURL: "https://opencode.ai/zen/go/v1"},
			want:    true,
		},
		{
			name:    "zen is a different product",
			profile: config.ProviderProfile{CatalogID: "opencode", BaseURL: "https://opencode.ai/zen/v1"},
			want:    false,
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

func TestHasKeyAndResolverReadAuthJSON(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("PATH", "")
	missing := filepath.Join(t.TempDir(), "missing.json")
	t.Setenv("OPENCODE_AUTH_PATH", missing)
	if HasKey(Options{}) {
		t.Fatal("HasKey() = true, want false with no auth file")
	}

	path := writeAuthFile(t, `{"opencode-go":{"type":"api","key":"sk-opencode-go-test"},"openrouter":{"type":"api","key":"sk-or-other"}}`)
	t.Setenv("OPENCODE_AUTH_PATH", path)
	if !HasKey(Options{}) {
		t.Fatal("HasKey() = false, want true when opencode-go key is present")
	}

	header, value, ok, err := Resolver(Options{})(context.Background(), false)
	if err != nil || !ok {
		t.Fatalf("Resolver() = (%v, %v)", ok, err)
	}
	if header != "Authorization" || value != "Bearer sk-opencode-go-test" {
		t.Fatalf("Resolver() = (%q, %q)", header, value)
	}
}

func TestResolverPrefersOPENCODE_API_KEY(t *testing.T) {
	path := writeAuthFile(t, `{"opencode-go":{"type":"api","key":"sk-from-file"}}`)
	t.Setenv("OPENCODE_AUTH_PATH", path)
	t.Setenv("OPENCODE_API_KEY", "sk-from-env")
	header, value, ok, err := Resolver(Options{})(context.Background(), false)
	if err != nil || !ok || header != "Authorization" || value != "Bearer sk-from-env" {
		t.Fatalf("Resolver() = (%q, %q, %v, %v)", header, value, ok, err)
	}
}

func TestResolverIgnoresEmptyOrMissingGoEntry(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	path := writeAuthFile(t, `{"openrouter":{"type":"api","key":"sk-or-other"}}`)
	t.Setenv("OPENCODE_AUTH_PATH", path)
	if HasKey(Options{}) {
		t.Fatal("HasKey() must ignore unrelated auth.json providers")
	}
	_, _, ok, err := Resolver(Options{})(context.Background(), false)
	if err != nil {
		t.Fatalf("Resolver() error = %v", err)
	}
	if ok {
		t.Fatal("Resolver() ok = true, want false when opencode-go key is absent")
	}
}

func TestResolverUsesAPIKeyHeaderForAnthropicCompatible(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	path := writeAuthFile(t, `{"opencode-go":{"type":"api","key":"sk-opencode-go-test"}}`)
	t.Setenv("OPENCODE_AUTH_PATH", path)
	header, value, ok, err := Resolver(Options{APIKeyHeader: true})(context.Background(), false)
	if err != nil || !ok {
		t.Fatalf("Resolver() = (%v, %v)", ok, err)
	}
	if header != "x-api-key" || value != "sk-opencode-go-test" {
		t.Fatalf("Resolver() = (%q, %q), want x-api-key with the raw key", header, value)
	}
}

func writeAuthFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(contents+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
