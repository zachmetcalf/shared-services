package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type refreshAccessTokenErrorProvider struct{}

func (*refreshAccessTokenErrorProvider) AccessToken(context.Context) (string, error) {
	return "", errors.New("token fetch failed")
}

func (*refreshAccessTokenErrorProvider) ForceRefresh(context.Context) error {
	return nil
}

func newTestApp(t *testing.T, cfg config) *app {
	t.Helper()

	a, err := newApp(cfg)
	if err != nil {
		t.Fatalf("new app failed (error: %v)", err)
	}

	return a
}

func newAppWithBotToken(t *testing.T, apiUrl string, botToken string) *app {
	return newTestApp(t, config{
		SlackApiUrl:   apiUrl,
		SlackBotToken: botToken,
	})
}

func newAppWithBotTokenAndProxyKey(t *testing.T, apiUrl string, botToken string, proxyKey string) *app {
	return newTestApp(t, config{
		SlackApiUrl:   apiUrl,
		SlackBotToken: botToken,
		SlackProxyKey: proxyKey,
	})
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config
		wantErr bool
	}{
		{
			name: "valid static token config",
			cfg: config{
				SlackApiUrl:   "https://slack.com/api",
				SlackBotToken: "xoxb-token",
			},
		},
		{
			name: "valid token rotation config",
			cfg: config{
				SlackApiUrl:       "https://slack.com/api",
				SlackClientId:     "client-id",
				SlackClientSecret: "client-secret",
				SlackRefreshToken: "refresh-token",
			},
		},
		{
			name: "missing auth config",
			cfg: config{
				SlackApiUrl: "https://slack.com/api",
			},
			wantErr: true,
		},
		{
			name: "missing api url",
			cfg: config{
				SlackBotToken: "xoxb-token",
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := validateConfig(test.cfg)
			gotErr := err != nil
			if gotErr != test.wantErr {
				t.Fatalf("validate config error: got %t, want %t (err: %v)", gotErr, test.wantErr, err)
			}
		})
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv(kEnvSlackApiUrl, "https://example.com/api")
	t.Setenv(kEnvSlackBotToken, "xoxb-from-env")
	t.Setenv(kEnvSlackTokenStorePath, "custom-token-store.json")

	cfg := loadConfig()
	if got, want := cfg.SlackApiUrl, "https://example.com/api"; got != want {
		t.Fatalf("slack api url: got %s, want %s", got, want)
	}
	if got, want := cfg.SlackBotToken, "xoxb-from-env"; got != want {
		t.Fatalf("bot token: got %s, want %s", got, want)
	}
	if got, want := cfg.SlackTokenStorePath, "custom-token-store.json"; got != want {
		t.Fatalf("token store path: got %s, want %s", got, want)
	}
}

func TestPing(t *testing.T) {
	a := newTestApp(t, config{})

	req := httptest.NewRequest(http.MethodGet, kRouteProxyPing, nil)
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := rec.Body.String(), `"service":"slack-proxy"`; !strings.Contains(got, want) {
		t.Fatalf("response body: got %q, want contains %q", got, want)
	}
}

func TestPingMethodNotAllowed(t *testing.T) {
	a := newTestApp(t, config{})

	req := httptest.NewRequest(http.MethodPost, kRouteProxyPing, nil)
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusMethodNotAllowed; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := rec.Header().Get("Allow"), http.MethodGet; got != want {
		t.Fatalf("allow header: got %s, want %s", got, want)
	}
	if got, want := rec.Body.String(), `"error":"method_not_allowed"`; !strings.Contains(got, want) {
		t.Fatalf("response body: got %q, want contains %q", got, want)
	}
}

func TestAuthTestForwards(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotAuth string

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", kJsonContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	a := newAppWithBotToken(t, api.URL, "xoxb-test")

	req := httptest.NewRequest(http.MethodGet, kRouteProxyAuthTest, nil)
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := gotMethod, http.MethodGet; got != want {
		t.Fatalf("method: got %s, want %s", got, want)
	}
	wantPath := "/" + kSlackMethodAuthTest
	if got, want := gotPath, wantPath; got != want {
		t.Fatalf("path: got %s, want %s", got, want)
	}
	if got, want := gotAuth, "Bearer xoxb-test"; got != want {
		t.Fatalf("auth header: got %s, want %s", got, want)
	}
}

func TestAuthTestMethodNotAllowed(t *testing.T) {
	a := newAppWithBotToken(t, "https://example.com/api", "xoxb-test")

	req := httptest.NewRequest(http.MethodPost, kRouteProxyAuthTest, nil)
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusMethodNotAllowed; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := rec.Header().Get("Allow"), http.MethodGet; got != want {
		t.Fatalf("allow header: got %s, want %s", got, want)
	}
}

func TestAuthTestRequiresProxyAuth(t *testing.T) {
	var gotCalls int

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCalls++
		w.Header().Set("Content-Type", kJsonContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	a := newAppWithBotTokenAndProxyKey(t, api.URL, "xoxb-test", "proxy-key")

	req := httptest.NewRequest(http.MethodGet, kRouteProxyAuthTest, nil)
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := rec.Body.String(), `"error":"invalid_proxy_auth"`; !strings.Contains(got, want) {
		t.Fatalf("response body: got %q, want contains %q", got, want)
	}
	if got, want := gotCalls, 0; got != want {
		t.Fatalf("api calls: got %d, want %d", got, want)
	}
}

func TestAuthTestReturnsTokenError(t *testing.T) {
	a := newTestApp(t, config{
		SlackApiUrl: "https://example.com/api",
	})

	req := httptest.NewRequest(http.MethodGet, kRouteProxyAuthTest, nil)
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := rec.Body.String(), `"error":"missing_slack_auth"`; !strings.Contains(got, want) {
		t.Fatalf("response body: got %q, want contains %q", got, want)
	}
}

func TestAuthTestReturnsSlackRequestFailure(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request (path: %s)", r.URL.Path)
	}))
	api.Close()

	a := newAppWithBotToken(t, api.URL, "xoxb-test")

	req := httptest.NewRequest(http.MethodGet, kRouteProxyAuthTest, nil)
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusBadGateway; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := rec.Body.String(), `"error":"slack_request_failed"`; !strings.Contains(got, want) {
		t.Fatalf("response body: got %q, want contains %q", got, want)
	}
}

func TestAuthTestForwardsWithProxyAuth(t *testing.T) {
	var gotCalls int
	var gotAuth string

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCalls++
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", kJsonContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	a := newAppWithBotTokenAndProxyKey(t, api.URL, "xoxb-test", "proxy-key")

	req := httptest.NewRequest(http.MethodGet, kRouteProxyAuthTest, nil)
	req.Header.Set("Authorization", "Bearer proxy-key")
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := gotCalls, 1; got != want {
		t.Fatalf("api calls: got %d, want %d", got, want)
	}
	if got, want := gotAuth, "Bearer xoxb-test"; got != want {
		t.Fatalf("forwarded slack auth header: got %s, want %s", got, want)
	}
}

func TestShouldRefreshToken(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       bool
	}{
		{
			name:       "unauthorized status",
			statusCode: http.StatusUnauthorized,
			want:       true,
		},
		{
			name:       "forbidden status",
			statusCode: http.StatusForbidden,
			want:       true,
		},
		{
			name:       "invalid json",
			statusCode: http.StatusOK,
			body:       "{",
		},
		{
			name:       "ok response",
			statusCode: http.StatusOK,
			body:       `{"ok":true}`,
		},
		{
			name:       "empty error",
			statusCode: http.StatusOK,
			body:       `{"ok":false}`,
		},
		{
			name:       "token error",
			statusCode: http.StatusOK,
			body:       `{"ok":false,"error":"token_expired"}`,
			want:       true,
		},
		{
			name:       "auth error",
			statusCode: http.StatusOK,
			body:       `{"ok":false,"error":"invalid_auth"}`,
			want:       true,
		},
		{
			name:       "non auth error",
			statusCode: http.StatusOK,
			body:       `{"ok":false,"error":"channel_not_found"}`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			got := shouldRefreshToken(test.statusCode, []byte(test.body))
			if got != test.want {
				t.Fatalf("should refresh token: got %t, want %t", got, test.want)
			}
		})
	}
}

func TestChatPostMessageForwards(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotAuth string
	var gotBody string

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("request body read failed (error: %v)", err)
			return
		}
		gotBody = string(body)

		w.Header().Set("Content-Type", kJsonContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	a := newAppWithBotToken(t, api.URL, "xoxb-test")

	payload := `{"channel":"C1","thread_ts":"123.456","text":"hello"}`
	req := httptest.NewRequest(http.MethodPost, kRouteProxyChatPostMessage, strings.NewReader(payload))
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := gotMethod, http.MethodPost; got != want {
		t.Fatalf("method: got %s, want %s", got, want)
	}
	wantPath := "/" + kSlackMethodChatPostMessage
	if got, want := gotPath, wantPath; got != want {
		t.Fatalf("path: got %s, want %s", got, want)
	}
	if got, want := gotAuth, "Bearer xoxb-test"; got != want {
		t.Fatalf("auth header: got %s, want %s", got, want)
	}
	if got, want := gotBody, payload; got != want {
		t.Fatalf("forwarded body: got %s, want %s", got, want)
	}
}

func TestChatPostMessageKeepsAuthErrorWhenRefreshFails(t *testing.T) {
	var gotChatCalls int

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotChatCalls++
		w.Header().Set("Content-Type", kJsonContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":false,"error":"token_expired"}`))
	}))
	defer api.Close()

	a := newAppWithBotToken(t, api.URL, "xoxb-test")

	req := httptest.NewRequest(http.MethodPost, kRouteProxyChatPostMessage, strings.NewReader(`{"channel":"C1","text":"hello"}`))
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := gotChatCalls, 1; got != want {
		t.Fatalf("chat calls: got %d, want %d", got, want)
	}
	if got, want := rec.Body.String(), `"error":"token_expired"`; !strings.Contains(got, want) {
		t.Fatalf("response body: got %q, want contains %q", got, want)
	}
}

func TestRetryAfterAuthErrorKeepsResponseWhenTokenFetchFails(t *testing.T) {
	a := &app{
		cfg: config{
			SlackApiUrl: "https://example.com/api",
		},
		client:        &http.Client{Timeout: kDefaultHttpTimeout},
		tokenProvider: &refreshAccessTokenErrorProvider{},
	}

	originalBody := []byte(`{"ok":false,"error":"token_expired"}`)
	req := httptest.NewRequest(http.MethodPost, kRouteProxyChatPostMessage, strings.NewReader(`{"channel":"C1","text":"hello"}`))

	statusCode, contentType, body := a.retryAfterAuthError(
		req,
		kSlackMethodChatPostMessage,
		[]byte(`{"channel":"C1","text":"hello"}`),
		http.StatusOK,
		kJsonContentType,
		originalBody,
	)

	if got, want := statusCode, http.StatusOK; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := contentType, kJsonContentType; got != want {
		t.Fatalf("content type: got %s, want %s", got, want)
	}
	if got, want := string(body), string(originalBody); got != want {
		t.Fatalf("response body: got %s, want %s", got, want)
	}
}

func TestChatPostMessageRefreshesAndRetriesOnAuthError(t *testing.T) {
	var gotChatCalls int
	var gotRefreshCalls int
	var gotAuthHeaders []string

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + kSlackMethodChatPostMessage:
			gotChatCalls++
			gotAuthHeaders = append(gotAuthHeaders, r.Header.Get("Authorization"))

			w.Header().Set("Content-Type", kJsonContentType)
			if gotChatCalls == 1 {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":false,"error":"token_expired"}`))
				return
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))

		case kRouteSlackOAuthV2Access:
			gotRefreshCalls++
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("oauth request body read failed (error: %v)", err)
				return
			}
			form, err := url.ParseQuery(string(body))
			if err != nil {
				t.Errorf("oauth form parse failed (error: %v)", err)
				return
			}
			if got, want := form.Get("grant_type"), "refresh_token"; got != want {
				t.Errorf("grant type mismatch (got: %s, want: %s)", got, want)
				return
			}
			if got, want := form.Get("refresh_token"), "xoxe-1-refresh"; got != want {
				t.Errorf("refresh token mismatch (got: %s, want: %s)", got, want)
				return
			}

			w.Header().Set("Content-Type", kJsonContentType)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"access_token":"xoxe.xoxb-new","refresh_token":"xoxe-1-refresh-2","expires_in":43200}`))

		default:
			t.Errorf("unexpected path (path: %s)", r.URL.Path)
		}
	}))
	defer api.Close()

	a := newTestApp(t, config{
		SlackApiUrl:       api.URL,
		SlackClientId:     "client-id",
		SlackClientSecret: "client-secret",
		SlackBotToken:     "xoxe.xoxb-old",
		SlackRefreshToken: "xoxe-1-refresh",
	})

	req := httptest.NewRequest(http.MethodPost, kRouteProxyChatPostMessage, strings.NewReader(`{"channel":"C1","text":"hello"}`))
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := gotRefreshCalls, 1; got != want {
		t.Fatalf("refresh calls: got %d, want %d", got, want)
	}
	if got, want := gotChatCalls, 2; got != want {
		t.Fatalf("chat calls: got %d, want %d", got, want)
	}
	if got, want := len(gotAuthHeaders), 2; got != want {
		t.Fatalf("auth header count: got %d, want %d", got, want)
	}
	if got, want := gotAuthHeaders[0], "Bearer xoxe.xoxb-old"; got != want {
		t.Fatalf("first auth header: got %s, want %s", got, want)
	}
	if got, want := gotAuthHeaders[1], "Bearer xoxe.xoxb-new"; got != want {
		t.Fatalf("second auth header: got %s, want %s", got, want)
	}
}

func TestFilesInfoForwardsQuery(t *testing.T) {
	var gotPath string
	var gotQuery string

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery

		w.Header().Set("Content-Type", kJsonContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	a := newAppWithBotToken(t, api.URL, "xoxb-test")

	req := httptest.NewRequest(http.MethodGet, kRouteProxyFilesInfo+"?file=F123", nil)
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	wantPath := "/" + kSlackMethodFilesInfo
	if got, want := gotPath, wantPath; got != want {
		t.Fatalf("path: got %s, want %s", got, want)
	}
	if got, want := gotQuery, "file=F123"; got != want {
		t.Fatalf("query: got %s, want %s", got, want)
	}
}
