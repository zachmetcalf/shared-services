package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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

	kSlackMethodAuthTest        = "auth.test"
	kSlackMethodChatPostMessage = "chat.postMessage"
	kSlackMethodChatUpdate      = "chat.update"
	kSlackMethodChatDelete      = "chat.delete"
	kSlackMethodFilesGetUpload  = "files.getUploadURLExternal"
	kSlackMethodFilesComplete   = "files.completeUploadExternal"
	kSlackMethodFilesInfo       = "files.info"
	kSlackMethodReactionsAdd    = "reactions.add"
	kSlackMethodReactionsRemove = "reactions.remove"

	kRouteProxyAuthTest        = kRouteSlackPrefix + kSlackMethodAuthTest
	kRouteProxyChatPostMessage = kRouteSlackPrefix + kSlackMethodChatPostMessage
	kRouteProxyChatUpdate      = kRouteSlackPrefix + kSlackMethodChatUpdate
	kRouteProxyChatDelete      = kRouteSlackPrefix + kSlackMethodChatDelete
	kRouteProxyFilesGetUpload  = kRouteSlackPrefix + kSlackMethodFilesGetUpload
	kRouteProxyFilesComplete   = kRouteSlackPrefix + kSlackMethodFilesComplete
	kRouteProxyFilesInfo       = kRouteSlackPrefix + kSlackMethodFilesInfo
	kRouteProxyReactionsAdd    = kRouteSlackPrefix + kSlackMethodReactionsAdd
	kRouteProxyReactionsRemove = kRouteSlackPrefix + kSlackMethodReactionsRemove

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
		return fmt.Errorf("missing env vars (required: SLACK_BOT_TOKEN or SLACK_CLIENT_ID, SLACK_CLIENT_SECRET, SLACK_REFRESH_TOKEN)")
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
	}

	tokenProvider, err := newTokenProvider(cfg, client)
	if err != nil {
		return nil, err
	}

	return &app{
		cfg:           cfg,
		client:        client,
		tokenProvider: tokenProvider,
	}, nil
}

func (a *app) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(kRouteProxyPing, a.handlePing)
	mux.HandleFunc(kRouteProxyAuthTest, a.handleMethod(http.MethodGet, kSlackMethodAuthTest))
	mux.HandleFunc(kRouteProxyChatPostMessage, a.handleMethod(http.MethodPost, kSlackMethodChatPostMessage))
	mux.HandleFunc(kRouteProxyChatUpdate, a.handleMethod(http.MethodPost, kSlackMethodChatUpdate))
	mux.HandleFunc(kRouteProxyChatDelete, a.handleMethod(http.MethodPost, kSlackMethodChatDelete))
	mux.HandleFunc(kRouteProxyFilesGetUpload, a.handleMethod(http.MethodGet, kSlackMethodFilesGetUpload))
	mux.HandleFunc(kRouteProxyFilesComplete, a.handleMethod(http.MethodPost, kSlackMethodFilesComplete))
	mux.HandleFunc(kRouteProxyFilesInfo, a.handleMethod(http.MethodGet, kSlackMethodFilesInfo))
	mux.HandleFunc(kRouteProxyReactionsAdd, a.handleMethod(http.MethodPost, kSlackMethodReactionsAdd))
	mux.HandleFunc(kRouteProxyReactionsRemove, a.handleMethod(http.MethodPost, kSlackMethodReactionsRemove))

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

	requestBody, err := readRequestBody(r, r.Method)
	if err != nil {
		writeError(w, http.StatusBadRequest, kErrorInvalidRequestBody)
		return
	}

	token, err := a.tokenProvider.AccessToken(r.Context())
	if err != nil {
		log.Printf("token acquisition failed (method: %s, error: %v)", method, err)
		writeJson(w, http.StatusInternalServerError, map[string]string{
			"error":  kErrorMissingSlackAuth,
			"reason": err.Error(),
		})
		return
	}

	statusCode, contentType, responseBody, err := a.forwardOnce(r, method, requestBody, token)
	if err != nil {
		log.Printf("request failed (method: %s, error: %v)", method, err)
		writeError(w, http.StatusBadGateway, kErrorSlackRequestFailed)
		return
	}

	statusCode, contentType, responseBody = a.retryAfterAuthError(r, method, requestBody, statusCode, contentType, responseBody)

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)
	_, _ = w.Write(responseBody)

	log.Printf("request forwarded (method: %s, status: %d)", method, statusCode)
}

func (a *app) isProxyAuthorized(r *http.Request) bool {
	if isBlank(a.cfg.SlackProxyKey) {
		return true
	}

	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	return auth == formatBearerAuth(a.cfg.SlackProxyKey)
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

	lower := strings.ToLower(strings.TrimSpace(payload.Error))
	if lower == "" {
		return false
	}

	return strings.Contains(lower, "auth") || strings.Contains(lower, "token")
}

func (a *app) retryAfterAuthError(
	r *http.Request,
	method string,
	requestBody []byte,
	statusCode int,
	contentType string,
	responseBody []byte,
) (int, string, []byte) {
	if !shouldRefreshToken(statusCode, responseBody) {
		return statusCode, contentType, responseBody
	}

	log.Printf("token refresh requested (method: %s, status: %d)", method, statusCode)

	if err := a.tokenProvider.ForceRefresh(r.Context()); err != nil {
		log.Printf("token refresh failed (method: %s, error: %v)", method, err)
		return statusCode, contentType, responseBody
	}

	token, err := a.tokenProvider.AccessToken(r.Context())
	if err != nil {
		log.Printf("token fetch after refresh failed (method: %s, error: %v)", method, err)
		return statusCode, contentType, responseBody
	}

	refreshedStatusCode, refreshedContentType, refreshedBody, err := a.forwardOnce(r, method, requestBody, token)
	if err != nil {
		log.Printf("request retry failed (method: %s, error: %v)", method, err)
		return statusCode, contentType, responseBody
	}

	return refreshedStatusCode, refreshedContentType, refreshedBody
}

func (a *app) forwardOnce(r *http.Request, method string, payload []byte, token string) (int, string, []byte, error) {
	endpointUrl := strings.TrimRight(a.cfg.SlackApiUrl, "/") + "/" + method

	var body io.Reader
	if r.Method == http.MethodPost {
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, endpointUrl, body)
	if err != nil {
		return 0, "", nil, err
	}

	if r.Method == http.MethodGet {
		req.URL.RawQuery = r.URL.Query().Encode()
	} else {
		req.Header.Set("Content-Type", kJsonContentType)
	}
	req.Header.Set("Authorization", formatBearerAuth(token))

	resp, err := a.client.Do(req)
	if err != nil {
		return 0, "", nil, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", nil, err
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = kJsonContentType
	}

	return resp.StatusCode, contentType, responseBody, nil
}

func main() {
	if err := loadDotEnvIfExists(kDefaultDotEnvPath); err != nil {
		log.Printf("%s startup failed (error: %v)", kServiceName, err)
		os.Exit(1)
	}

	cfg := loadConfig()
	if err := validateConfig(cfg); err != nil {
		log.Printf("%s startup failed (error: %v)", kServiceName, err)
		os.Exit(1)
	}

	log.Printf("%s listening (listen_address: %s, slack_api_url: %s)", kServiceName, kDefaultListenAddress, cfg.SlackApiUrl)

	app, err := newApp(cfg)
	if err != nil {
		log.Printf("%s startup failed (error: %v)", kServiceName, err)
		os.Exit(1)
	}
	if err := http.ListenAndServe(kDefaultListenAddress, app.handler()); err != nil {
		log.Printf("%s stopped (error: %v)", kServiceName, err)
	}
}
