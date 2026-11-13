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
