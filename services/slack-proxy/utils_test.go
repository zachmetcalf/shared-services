package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
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
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file failed (error: %v)", err)
	}

	t.Setenv(kTestDotEnvOverrideKey, "from-env")
	t.Setenv(kTestDotEnvLoadedKey, "")
	if err := os.Unsetenv(kTestDotEnvLoadedKey); err != nil {
		t.Fatalf("unset env failed (key: %s, error: %v)", kTestDotEnvLoadedKey, err)
	}

	if err := loadDotEnvIfExists(path); err != nil {
		t.Fatalf("load dotenv failed (error: %v)", err)
	}
	if got, want := os.Getenv(kTestDotEnvOverrideKey), "from-env"; got != want {
		t.Fatalf("existing env override: got %s, want %s", got, want)
	}
	got, ok := os.LookupEnv(kTestDotEnvLoadedKey)
	if !ok {
		t.Fatalf("loaded env is missing (key: %s)", kTestDotEnvLoadedKey)
	}
	if want := ":9000"; got != want {
		t.Fatalf("loaded env value: got %s, want %s", got, want)
	}
}

func TestReadLimitedBody(t *testing.T) {
	tests := []struct {
		name    string
		reader  io.Reader
		want    string
		wantErr error
	}{
		{
			name:   "below limit",
			reader: strings.NewReader("ok"),
			want:   "ok",
		},
		{
			name:   "at limit",
			reader: strings.NewReader("body"),
			want:   "body",
		},
		{
			name:    "above limit",
			reader:  strings.NewReader("large"),
			wantErr: errBodyTooLarge,
		},
		{
			name:    "read failure",
			reader:  iotest.ErrReader(io.ErrUnexpectedEOF),
			wantErr: io.ErrUnexpectedEOF,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := readLimitedBody(test.reader, 4)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("read body error: got %v, want %v", err, test.wantErr)
			}
			if got, want := string(body), test.want; got != want {
				t.Fatalf("body: got %s, want %s", got, want)
			}
		})
	}
}

func TestErrorReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "body too large",
			err:  errBodyTooLarge,
			want: "body_too_large",
		},
		{
			name: "canceled",
			err:  context.Canceled,
			want: "canceled",
		},
		{
			name: "deadline exceeded",
			err:  context.DeadlineExceeded,
			want: "timeout",
		},
		{
			name: "network timeout",
			err:  &net.DNSError{Err: "timeout", IsTimeout: true},
			want: "timeout",
		},
		{
			name: "network error",
			err:  &net.DNSError{Err: "host unavailable"},
			want: "network_error",
		},
		{
			name: "wrapped URL error",
			err:  &url.Error{Op: "Get", URL: "https://example.invalid/?token=caller-token", Err: context.DeadlineExceeded},
			want: "timeout",
		},
		{
			name: "unknown error details",
			err:  errors.New("private-error-details"),
			want: "operation_failed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, want := errorReason(test.err), test.want; got != want {
				t.Fatalf("error reason: got %s, want %s", got, want)
			}
		})
	}
}
