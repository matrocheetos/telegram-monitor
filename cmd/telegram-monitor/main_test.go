package main

import (
	"os"
	"strings"
	"testing"
)

func TestIsLocalDebugCollectionEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		appEnv    string
		debugMode bool
		expected  bool
	}{
		{
			name:      "debug mode false always disabled",
			appEnv:    "local",
			debugMode: false,
			expected:  false,
		},
		{
			name:      "debug mode false with empty env",
			appEnv:    "",
			debugMode: false,
			expected:  false,
		},
		{
			name:      "debug mode true with empty env (local default)",
			appEnv:    "",
			debugMode: true,
			expected:  true,
		},
		{
			name:      "debug mode true with local env",
			appEnv:    "local",
			debugMode: true,
			expected:  true,
		},
		{
			name:      "debug mode true with development env",
			appEnv:    "development",
			debugMode: true,
			expected:  true,
		},
		{
			name:      "debug mode true with dev env",
			appEnv:    "dev",
			debugMode: true,
			expected:  true,
		},
		{
			name:      "debug mode true with mixed case Dev",
			appEnv:    "Dev",
			debugMode: true,
			expected:  true,
		},
		{
			name:      "debug mode true with production env is strictly BLOCKED",
			appEnv:    "production",
			debugMode: true,
			expected:  false,
		},
		{
			name:      "debug mode true with prod env is strictly BLOCKED",
			appEnv:    "prod",
			debugMode: true,
			expected:  false,
		},
		{
			name:      "debug mode true with staging env is strictly BLOCKED",
			appEnv:    "staging",
			debugMode: true,
			expected:  false,
		},
		{
			name:      "debug mode true with unknown env is strictly BLOCKED",
			appEnv:    "cloud-run",
			debugMode: true,
			expected:  false,
		},
	}

	for _, tc := range tests {
		actual := isLocalDebugCollectionEnabled(tc.appEnv, tc.debugMode)
		if actual != tc.expected {
			t.Errorf("%s: isLocalDebugCollectionEnabled(%q, %v) = %v; want %v",
				tc.name, tc.appEnv, tc.debugMode, actual, tc.expected)
		}
	}
}

func TestLoadDotEnvFromReader(t *testing.T) {
	t.Setenv("TEST_EXISTING_VAR", "already-set")
	t.Cleanup(func() {
		_ = os.Unsetenv("TEST_SIMPLE")
		_ = os.Unsetenv("TEST_QUOTED")
		_ = os.Unsetenv("TEST_SINGLE_QUOTED")
		_ = os.Unsetenv("TEST_EXPORTED")
		_ = os.Unsetenv("TEST_EMPTY")
	})

	input := "\xef\xbb\xbf# Comment line\n" +
		"\n" +
		"TEST_SIMPLE=development\n" +
		"TEST_QUOTED=\"https://ntfy.example.com/topic\"\n" +
		"TEST_SINGLE_QUOTED='val=with=equals'\n" +
		"export TEST_EXPORTED = test-\n" +
		"TEST_EMPTY=\n" +
		"TEST_EXISTING_VAR=from-dotenv\n"

	if err := loadDotEnvFromReader(strings.NewReader(input)); err != nil {
		t.Fatalf("loadDotEnvFromReader failed: %v", err)
	}

	checks := []struct {
		key  string
		want string
	}{
		{"TEST_SIMPLE", "development"},
		{"TEST_QUOTED", "https://ntfy.example.com/topic"},
		{"TEST_SINGLE_QUOTED", "val=with=equals"},
		{"TEST_EXPORTED", "test-"},
		{"TEST_EMPTY", ""},
		{"TEST_EXISTING_VAR", "already-set"},
	}

	for _, c := range checks {
		got, ok := os.LookupEnv(c.key)
		if !ok || got != c.want {
			t.Errorf("env %s = (%q, %v), want (%q, true)", c.key, got, ok, c.want)
		}
	}
}

func TestResolveSecretKey(t *testing.T) {
	t.Parallel()

	validProdKey := strings.Repeat("a", 32)
	shortProdKey := strings.Repeat("a", 31)

	tests := []struct {
		name      string
		appEnv    string
		rawKey    string
		wantKey   string
		wantError bool
	}{
		{
			name:    "empty env with empty key falls back to dev secret",
			appEnv:  "",
			rawKey:  "",
			wantKey: devFallbackSecretKey,
		},
		{
			name:    "local env with empty key falls back to dev secret",
			appEnv:  "local",
			rawKey:  "",
			wantKey: devFallbackSecretKey,
		},
		{
			name:    "development env with empty key falls back to dev secret",
			appEnv:  "development",
			rawKey:  "",
			wantKey: devFallbackSecretKey,
		},
		{
			name:    "dev env with empty key falls back to dev secret",
			appEnv:  "dev",
			rawKey:  "",
			wantKey: devFallbackSecretKey,
		},
		{
			name:    "test env with empty key falls back to dev secret",
			appEnv:  "test",
			rawKey:  "",
			wantKey: devFallbackSecretKey,
		},
		{
			name:    "local env with custom key returns custom key",
			appEnv:  "local",
			rawKey:  "my-custom-local-key",
			wantKey: "my-custom-local-key",
		},
		{
			name:      "production env with empty key fails",
			appEnv:    "production",
			rawKey:    "",
			wantError: true,
		},
		{
			name:      "prod env with dev fallback key fails",
			appEnv:    "prod",
			rawKey:    devFallbackSecretKey,
			wantError: true,
		},
		{
			name:      "staging env with short key fails",
			appEnv:    "staging",
			rawKey:    shortProdKey,
			wantError: true,
		},
		{
			name:      "production env with 31-byte key fails",
			appEnv:    "production",
			rawKey:    shortProdKey,
			wantError: true,
		},
		{
			name:    "production env with 32-byte key succeeds",
			appEnv:  "production",
			rawKey:  validProdKey,
			wantKey: validProdKey,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveSecretKey(tc.appEnv, tc.rawKey)
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error, got nil (key=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantKey {
				t.Fatalf("resolveSecretKey(%q, %q) = %q, want %q", tc.appEnv, tc.rawKey, got, tc.wantKey)
			}
		})
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		name       string
		val        string
		set        bool
		defaultVal bool
		want       bool
	}{
		{name: "unset returns default true", set: false, defaultVal: true, want: true},
		{name: "unset returns default false", set: false, defaultVal: false, want: false},
		{name: "empty returns default", val: "  ", set: true, defaultVal: true, want: true},
		{name: "true parses true", val: "true", set: true, defaultVal: false, want: true},
		{name: "1 parses true", val: "1", set: true, defaultVal: false, want: true},
		{name: "false parses false", val: "false", set: true, defaultVal: true, want: false},
		{name: "invalid returns default", val: "notabool", set: true, defaultVal: true, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const key = "TEST_ENV_BOOL_KEY"
			if tc.set {
				t.Setenv(key, tc.val)
			} else {
				_ = os.Unsetenv(key)
			}
			if got := envBool(key, tc.defaultVal); got != tc.want {
				t.Fatalf("envBool = %v, want %v", got, tc.want)
			}
		})
	}
}
