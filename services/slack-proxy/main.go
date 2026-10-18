package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	kServiceName           = "slack-proxy"
	kDefaultListenAddress  = ":8082"
	kDefaultSlackApiUrl    = "https://slack.com/api"
	kDefaultDotEnvPath     = ".env"
	kDefaultTokenStorePath = "data/token-store.json"
	kDefaultHttpTimeout    = 30 * time.Second
	kJsonContentType       = "application/json; charset=utf-8"

	kRouteProxyPing          = "/ping"
	kRouteSlackPrefix        = "/v1/slack/"
	kRouteSlackOAuthV2Access = "/oauth.v2.access"

	kSlackMethodAuthTest             = "auth.test"
	kSlackMethodChatPostMessage      = "chat.postMessage"
	kSlackMethodChatUpdate           = "chat.update"
	kSlackMethodChatDelete           = "chat.delete"
	kSlackMethodConversationsHistory = "conversations.history"
	kSlackMethodFilesGetUpload       = "files.getUploadURLExternal"
	kSlackMethodFilesComplete        = "files.completeUploadExternal"
	kSlackMethodFilesInfo            = "files.info"
	kSlackMethodReactionsAdd         = "reactions.add"
	kSlackMethodReactionsRemove      = "reactions.remove"
	kSlackMethodUsersConversations   = "users.conversations"

	kRouteProxyAuthTest             = kRouteSlackPrefix + kSlackMethodAuthTest
	kRouteProxyChatPostMessage      = kRouteSlackPrefix + kSlackMethodChatPostMessage
	kRouteProxyChatUpdate           = kRouteSlackPrefix + kSlackMethodChatUpdate
	kRouteProxyChatDelete           = kRouteSlackPrefix + kSlackMethodChatDelete
	kRouteProxyConversationsHistory = kRouteSlackPrefix + kSlackMethodConversationsHistory
	kRouteProxyFilesGetUpload       = kRouteSlackPrefix + kSlackMethodFilesGetUpload
	kRouteProxyFilesComplete        = kRouteSlackPrefix + kSlackMethodFilesComplete
	kRouteProxyFilesInfo            = kRouteSlackPrefix + kSlackMethodFilesInfo
	kRouteProxyReactionsAdd         = kRouteSlackPrefix + kSlackMethodReactionsAdd
	kRouteProxyReactionsRemove      = kRouteSlackPrefix + kSlackMethodReactionsRemove
	kRouteProxyUsersConversations   = kRouteSlackPrefix + kSlackMethodUsersConversations

	kEnvSlackApiUrl         = "SLACK_API_URL"
	kEnvSlackBotToken       = "SLACK_BOT_TOKEN"
	kEnvSlackClientId       = "SLACK_CLIENT_ID"
	kEnvSlackClientSecret   = "SLACK_CLIENT_SECRET"
	kEnvSlackRefreshToken   = "SLACK_REFRESH_TOKEN"
	kEnvSlackTokenStorePath = "SLACK_TOKEN_STORE_PATH"
	kEnvSlackProxyKey       = "SLACK_PROXY_KEY"

	kErrorMethodNotAllowed   = "method_not_allowed"
	kErrorInvalidRequestBody = "invalid_request_body"
	kErrorSlackRequestFailed = "slack_request_failed"
	kErrorInvalidProxyAuth   = "invalid_proxy_auth"
)

type config struct {
	SlackApiUrl         string
	SlackBotToken       string
	SlackClientId       string
	SlackClientSecret   string
	SlackRefreshToken   string
	SlackTokenStorePath string
	SlackProxyKey       string
}

type app struct {
	cfg           config
	client        *http.Client
	tokenProvider tokenProvider
}

type slackResponse struct {
	StatusCode  int
	ContentType string
	RetryAfter  string
	Body        []byte
}

func loadConfig() config {
	return config{
		SlackApiUrl:         getEnvOrDefault(kEnvSlackApiUrl, kDefaultSlackApiUrl),
		SlackBotToken:       getEnvTrimmed(kEnvSlackBotToken),
		SlackClientId:       getEnvTrimmed(kEnvSlackClientId),
		SlackClientSecret:   getEnvTrimmed(kEnvSlackClientSecret),
		SlackRefreshToken:   getEnvTrimmed(kEnvSlackRefreshToken),
		SlackTokenStorePath: getEnvOrDefault(kEnvSlackTokenStorePath, kDefaultTokenStorePath),
		SlackProxyKey:       getEnvTrimmed(kEnvSlackProxyKey),
	}
}

func validateConfig(cfg config) error {
	if isBlank(cfg.SlackApiUrl) {
		return fmt.Errorf("missing config value (key: %s)", kEnvSlackApiUrl)
	}
	if !hasAuthConfig(cfg) {
		return errors.New("missing env vars (required: SLACK_BOT_TOKEN or SLACK_CLIENT_ID, SLACK_CLIENT_SECRET, SLACK_REFRESH_TOKEN)")
	}

	return nil
}

func hasAuthConfig(cfg config) bool {
	if !isBlank(cfg.SlackBotToken) {
		return true
	}

	return hasTokenRotationConfig(cfg)
}

func newApp(cfg config) (*app, error) {
	client := &http.Client{
		Timeout: kDefaultHttpTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	provider, err := newTokenProvider(cfg, client)
	if err != nil {
		return nil, err
	}

	return &app{
		cfg:           cfg,
		client:        client,
		tokenProvider: provider,
	}, nil
}

func (a *app) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(kRouteProxyPing, a.handlePing)
	mux.HandleFunc(kRouteProxyAuthTest, a.handleMethod(http.MethodGet, kSlackMethodAuthTest))
	mux.HandleFunc(kRouteProxyChatPostMessage, a.handleMethod(http.MethodPost, kSlackMethodChatPostMessage))
	mux.HandleFunc(kRouteProxyChatUpdate, a.handleMethod(http.MethodPost, kSlackMethodChatUpdate))
	mux.HandleFunc(kRouteProxyChatDelete, a.handleMethod(http.MethodPost, kSlackMethodChatDelete))
	mux.HandleFunc(kRouteProxyConversationsHistory, a.handleMethod(http.MethodPost, kSlackMethodConversationsHistory))
	mux.HandleFunc(kRouteProxyFilesGetUpload, a.handleMethod(http.MethodGet, kSlackMethodFilesGetUpload))
	mux.HandleFunc(kRouteProxyFilesComplete, a.handleMethod(http.MethodPost, kSlackMethodFilesComplete))
	mux.HandleFunc(kRouteProxyFilesInfo, a.handleMethod(http.MethodGet, kSlackMethodFilesInfo))
	mux.HandleFunc(kRouteProxyReactionsAdd, a.handleMethod(http.MethodPost, kSlackMethodReactionsAdd))
	mux.HandleFunc(kRouteProxyReactionsRemove, a.handleMethod(http.MethodPost, kSlackMethodReactionsRemove))
	mux.HandleFunc(kRouteProxyUsersConversations, a.handleMethod(http.MethodGet, kSlackMethodUsersConversations))

	return mux
}

func (a *app) handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	writeJson(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": kServiceName,
	})
}

func (a *app) handleMethod(allowedMethod string, method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != allowedMethod {
			writeMethodNotAllowed(w, allowedMethod)
			return
		}

		a.forward(w, r, method)
	}
}

func (a *app) forward(w http.ResponseWriter, r *http.Request, method string) {
	if !a.isProxyAuthorized(r) {
		writeError(w, http.StatusUnauthorized, kErrorInvalidProxyAuth)
		return
	}

	requestBody, err := readRequestBody(r)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errBodyTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		log.Printf("request body read failed (method: %s, reason: %s)", method, errorReason(err))
		writeError(w, status, kErrorInvalidRequestBody)
		return
	}

	token, err := a.tokenProvider.AccessToken(r.Context())
	if err != nil {
		log.Printf("token fetch failed (method: %s, reason: %s)", method, errorReason(err))
		writeError(w, http.StatusInternalServerError, kErrorMissingSlackAuth)
		return
	}

	response, err := a.forwardOnce(r, method, requestBody, token)
	if err != nil {
		log.Printf("request failed (method: %s, reason: %s)", method, errorReason(err))
		writeError(w, http.StatusBadGateway, kErrorSlackRequestFailed)
		return
	}

	response = a.retryAfterAuthError(r, method, requestBody, response)

	w.Header().Set("Content-Type", response.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	if value := response.RetryAfter; value != "" {
		w.Header().Set("Retry-After", value)
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(response.Body)

	log.Printf("request forwarded (method: %s, status_code: %d)", method, response.StatusCode)
}

func (a *app) isProxyAuthorized(r *http.Request) bool {
	if isBlank(a.cfg.SlackProxyKey) {
		return true
	}

	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	return subtle.ConstantTimeCompare([]byte(auth), []byte(formatBearerAuth(a.cfg.SlackProxyKey))) == 1
}

func shouldRefreshToken(statusCode int, responseBody []byte) bool {
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return true
	}

	var payload struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return false
	}
	if payload.OK {
		return false
	}

	code := strings.ToLower(payload.Error)
	return strings.Contains(code, "auth") || strings.Contains(code, "token")
}

func (a *app) retryAfterAuthError(r *http.Request, method string, requestBody []byte, response slackResponse) slackResponse {
	if !shouldRefreshToken(response.StatusCode, response.Body) {
		return response
	}

	log.Printf("token refresh requested (method: %s, status_code: %d)", method, response.StatusCode)

	if err := a.tokenProvider.ForceRefresh(r.Context()); err != nil {
		log.Printf("token refresh failed (method: %s, reason: %s)", method, errorReason(err))
		return response
	}

	token, err := a.tokenProvider.AccessToken(r.Context())
	if err != nil {
		log.Printf("token fetch after refresh failed (method: %s, reason: %s)", method, errorReason(err))
		return response
	}

	refreshed, err := a.forwardOnce(r, method, requestBody, token)
	if err != nil {
		log.Printf("request retry failed (method: %s, reason: %s)", method, errorReason(err))
		return response
	}

	return refreshed
}

func (a *app) forwardOnce(r *http.Request, method string, requestBody []byte, token string) (slackResponse, error) {
	endpoint := strings.TrimRight(a.cfg.SlackApiUrl, "/") + "/" + method

	var body io.Reader
	if r.Method == http.MethodPost {
		body = bytes.NewReader(requestBody)
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint, body)
	if err != nil {
		return slackResponse{}, err
	}

	if r.Method == http.MethodGet {
		req.URL.RawQuery = r.URL.Query().Encode()
	} else {
		req.Header.Set("Content-Type", kJsonContentType)
	}
	req.Header.Set("Authorization", formatBearerAuth(token))

	resp, err := a.client.Do(req)
	if err != nil {
		return slackResponse{}, err
	}
	defer resp.Body.Close()

	responseBody, err := readLimitedBody(resp.Body, kMaximumResponseBytes)
	if err != nil {
		return slackResponse{}, err
	}

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return slackResponse{}, errors.New("upstream redirect rejected")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = kJsonContentType
	}

	return slackResponse{
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		RetryAfter:  resp.Header.Get("Retry-After"),
		Body:        responseBody,
	}, nil
}

func main() {
	log.SetPrefix("[" + kServiceName + "] ")

	if err := loadDotEnvIfExists(kDefaultDotEnvPath); err != nil {
		log.Fatalf("startup failed (error: %v)", err)
	}

	cfg := loadConfig()
	if err := validateConfig(cfg); err != nil {
		log.Fatalf("startup failed (error: %v)", err)
	}

	app, err := newApp(cfg)
	if err != nil {
		log.Fatalf("startup failed (error: %v)", err)
	}

	server := &http.Server{
		Addr:              kDefaultListenAddress,
		Handler:           app.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       kDefaultHttpTimeout,
		WriteTimeout:      2 * kDefaultHttpTimeout,
		IdleTimeout:       time.Minute,
	}

	log.Printf("starting (listen_address: %s, slack_api_url: %s)", kDefaultListenAddress, cfg.SlackApiUrl)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("server failed (error: %v)", err)
	}
}
