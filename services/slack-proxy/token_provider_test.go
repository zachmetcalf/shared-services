package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestTokenProvider(t *testing.T, cfg config) tokenProvider {
	t.Helper()

	client := &http.Client{
		Timeout: kDefaultHttpTimeout,
	}

	provider, err := newTokenProvider(cfg, client)
	if err != nil {
		t.Fatalf("new token provider failed (error: %v)", err)
	}

	return provider
}

func TestTokenProviderLoadsTokenStoreOnStartup(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "token-store.json")
	storePayload := map[string]string{
		"access_token":   "xoxe.xoxb-stored",
		"refresh_token":  "xoxe-1-stored",
		"expires_at_utc": time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339),
	}
	storeBytes, err := json.Marshal(storePayload)
	if err != nil {
		t.Fatalf("token store encode failed (error: %v)", err)
	}
	if err := os.WriteFile(storePath, storeBytes, 0o600); err != nil {
		t.Fatalf("token store write failed (error: %v)", err)
	}

	provider := newTestTokenProvider(t, config{
		SlackClientId:       "client-id",
		SlackClientSecret:   "client-secret",
		SlackBotToken:       "xoxe.xoxb-env",
		SlackRefreshToken:   "xoxe-1-env",
		SlackTokenStorePath: storePath,
	})

	token, err := provider.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("access token failed (error: %v)", err)
	}
	if got, want := token, "xoxe.xoxb-stored"; got != want {
		t.Fatalf("access token: got %s, want %s", got, want)
	}
}

func TestTokenProviderRefreshPersistsTokenStore(t *testing.T) {
	var gotRefreshToken string

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, kRouteSlackOAuthV2Access; got != want {
			t.Fatalf("path: got %s, want %s", got, want)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("oauth request body read failed (error: %v)", err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("oauth form parse failed (error: %v)", err)
		}
		gotRefreshToken = form.Get("refresh_token")

		w.Header().Set("Content-Type", kJsonContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"access_token":"xoxe.xoxb-new","refresh_token":"xoxe-1-refresh-2","expires_in":43200}`))
	}))
	defer api.Close()

	storePath := filepath.Join(t.TempDir(), "token-store.json")
	provider := newTestTokenProvider(t, config{
		SlackApiUrl:         api.URL,
		SlackClientId:       "client-id",
		SlackClientSecret:   "client-secret",
		SlackRefreshToken:   "xoxe-1-refresh",
		SlackTokenStorePath: storePath,
	})

	if err := provider.ForceRefresh(context.Background()); err != nil {
		t.Fatalf("force refresh failed (error: %v)", err)
	}
	if got, want := gotRefreshToken, "xoxe-1-refresh"; got != want {
		t.Fatalf("refresh token: got %s, want %s", got, want)
	}

	storeBytes, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("token store read failed (error: %v)", err)
	}

	var store struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAtUTC string `json:"expires_at_utc"`
	}
	if err := json.Unmarshal(storeBytes, &store); err != nil {
		t.Fatalf("token store parse failed (error: %v)", err)
	}
	if got, want := store.AccessToken, "xoxe.xoxb-new"; got != want {
		t.Fatalf("stored access token: got %s, want %s", got, want)
	}
	if got, want := store.RefreshToken, "xoxe-1-refresh-2"; got != want {
		t.Fatalf("stored refresh token: got %s, want %s", got, want)
	}
	if store.ExpiresAtUTC == "" {
		t.Fatalf("stored expires_at_utc: got empty")
	}
	if _, err := time.Parse(time.RFC3339, store.ExpiresAtUTC); err != nil {
		t.Fatalf("stored expires_at_utc parse failed (error: %v)", err)
	}
}

func TestTokenProviderRefreshesExpiredTokenOnAccess(t *testing.T) {
	var gotRefreshCalls int

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRefreshCalls++
		w.Header().Set("Content-Type", kJsonContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"access_token":"xoxe.xoxb-new","refresh_token":"xoxe-1-refresh-2","expires_in":43200}`))
	}))
	defer api.Close()

	provider := newTestTokenProvider(t, config{
		SlackApiUrl:       api.URL,
		SlackClientId:     "client-id",
		SlackClientSecret: "client-secret",
		SlackBotToken:     "xoxe.xoxb-old",
		SlackRefreshToken: "xoxe-1-refresh",
	})

	rotatingProvider, ok := provider.(*rotatingTokenProvider)
	if !ok {
		t.Fatalf("provider type mismatch")
	}
	rotatingProvider.expiresAtUTC = time.Now().UTC().Add(1 * time.Minute)

	token, err := provider.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("access token failed (error: %v)", err)
	}
	if got, want := token, "xoxe.xoxb-new"; got != want {
		t.Fatalf("access token: got %s, want %s", got, want)
	}
	if got, want := gotRefreshCalls, 1; got != want {
		t.Fatalf("refresh calls: got %d, want %d", got, want)
	}
}

func TestTokenProviderReturnsErrorForInvalidTokenStore(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "token-store.json")
	if err := os.WriteFile(storePath, []byte("{"), 0o600); err != nil {
		t.Fatalf("token store write failed (error: %v)", err)
	}

	client := &http.Client{
		Timeout: kDefaultHttpTimeout,
	}

	_, err := newTokenProvider(config{
		SlackClientId:       "client-id",
		SlackClientSecret:   "client-secret",
		SlackRefreshToken:   "xoxe-1-refresh",
		SlackTokenStorePath: storePath,
	}, client)
	if err == nil {
		t.Fatalf("new token provider error: got nil, want non-nil")
	}
}

func TestStaticTokenProviderErrors(t *testing.T) {
	provider := newTestTokenProvider(t, config{})

	_, err := provider.AccessToken(context.Background())
	if err == nil {
		t.Fatalf("access token error: got nil, want non-nil")
	}

	if err := provider.ForceRefresh(context.Background()); err == nil {
		t.Fatalf("force refresh error: got nil, want non-nil")
	}
}

func TestTokenProviderReturnsErrorForInvalidTokenStoreExpiresAt(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "token-store.json")
	storePayload := map[string]string{
		"expires_at_utc": "invalid",
	}
	storeBytes, err := json.Marshal(storePayload)
	if err != nil {
		t.Fatalf("token store encode failed (error: %v)", err)
	}
	if err := os.WriteFile(storePath, storeBytes, 0o600); err != nil {
		t.Fatalf("token store write failed (error: %v)", err)
	}

	client := &http.Client{
		Timeout: kDefaultHttpTimeout,
	}

	_, err = newTokenProvider(config{
		SlackClientId:       "client-id",
		SlackClientSecret:   "client-secret",
		SlackRefreshToken:   "xoxe-1-refresh",
		SlackTokenStorePath: storePath,
	}, client)
	if err == nil {
		t.Fatalf("new token provider error: got nil, want non-nil")
	}
}

func TestTokenProviderRefreshReturnsErrorForOAuthFailures(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{
			name:       "invalid json",
			statusCode: http.StatusOK,
			body:       `{`,
		},
		{
			name:       "http error with slack error",
			statusCode: http.StatusBadRequest,
			body:       `{"ok":false,"error":"invalid_client_id"}`,
		},
		{
			name:       "http error without slack error",
			statusCode: http.StatusInternalServerError,
			body:       `{"ok":false}`,
		},
		{
			name:       "slack error",
			statusCode: http.StatusOK,
			body:       `{"ok":false,"error":"invalid_refresh_token"}`,
		},
		{
			name:       "missing access token",
			statusCode: http.StatusOK,
			body:       `{"ok":true}`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", kJsonContentType)
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.body))
			}))
			defer api.Close()

			provider := newTestTokenProvider(t, config{
				SlackApiUrl:       api.URL,
				SlackClientId:     "client-id",
				SlackClientSecret: "client-secret",
				SlackRefreshToken: "xoxe-1-refresh",
			})

			if err := provider.ForceRefresh(context.Background()); err == nil {
				t.Fatalf("force refresh error: got nil, want non-nil")
			}
		})
	}
}
