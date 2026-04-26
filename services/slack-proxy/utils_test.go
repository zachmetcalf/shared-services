package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	kTestDotEnvOverrideKey = "SLACK_PROXY_TEST_DOT_ENV_OVERRIDE"
	kTestDotEnvLoadedKey   = "SLACK_PROXY_TEST_DOT_ENV_LOADED"
)

func TestLoadDotEnvIfExistsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := loadDotEnvIfExists(path); err != nil {
		t.Fatalf("load dotenv failed (error: %v)", err)
	}
}

func TestLoadDotEnvIfExistsLoadsValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := strings.Join([]string{
		kTestDotEnvOverrideKey + "=from-file",
		kTestDotEnvLoadedKey + "=:9000",
		"MALFORMED",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file failed (error: %v)", err)
	}

	t.Setenv(kTestDotEnvOverrideKey, "from-env")
	if err := os.Unsetenv(kTestDotEnvLoadedKey); err != nil {
		t.Fatalf("unset env failed (key: %s, error: %v)", kTestDotEnvLoadedKey, err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv(kTestDotEnvLoadedKey)
	})

	if err := loadDotEnvIfExists(path); err != nil {
		t.Fatalf("load dotenv failed (error: %v)", err)
	}
	if got, want := os.Getenv(kTestDotEnvOverrideKey), "from-env"; got != want {
		t.Fatalf("existing env override mismatch (got: %s, want: %s)", got, want)
	}
	got, ok := os.LookupEnv(kTestDotEnvLoadedKey)
	if !ok {
		t.Fatalf("loaded env is missing (key: %s)", kTestDotEnvLoadedKey)
	}
	if want := ":9000"; got != want {
		t.Fatalf("loaded env value mismatch (got: %s, want: %s)", got, want)
	}
}
