package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	kTokenRefreshLeadTime = 5 * time.Minute
	kFormContentType      = "application/x-www-form-urlencoded"

	kOAuthFieldClientId     = "client_id"
	kOAuthFieldClientSecret = "client_secret"
	kOAuthFieldGrantType    = "grant_type"
	kOAuthRefreshToken      = "refresh_token"

	kErrorMissingSlackAuth     = "missing_slack_auth"
	kErrorMissingRefreshToken  = "missing_slack_refresh_token"
	kErrorRefreshNotConfigured = "slack_token_refresh_not_configured"
)

type tokenProvider interface {
	AccessToken(ctx context.Context) (string, error)
	ForceRefresh(ctx context.Context) error
}

type staticTokenProvider struct {
	token string
}

func (p *staticTokenProvider) AccessToken(context.Context) (string, error) {
	if isBlank(p.token) {
		return "", errors.New(kErrorMissingSlackAuth)
	}

	return p.token, nil
}

func (*staticTokenProvider) ForceRefresh(context.Context) error {
	return errors.New(kErrorRefreshNotConfigured)
}

type rotatingTokenProvider struct {
	cfg    config
	client *http.Client

	mu           sync.Mutex
	accessToken  string
	refreshToken string
	expiresAtUTC time.Time
}

type oauthResponse struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type tokenStorePayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAtUTC string `json:"expires_at_utc"`
}

func newTokenProvider(cfg config, client *http.Client) (tokenProvider, error) {
	if hasTokenRotationConfig(cfg) {
		p := &rotatingTokenProvider{
			cfg:          cfg,
			client:       client,
			accessToken:  cfg.SlackBotToken,
			refreshToken: cfg.SlackRefreshToken,
		}
		if err := p.loadTokenStore(); err != nil {
			return nil, err
		}
		return p, nil
	}

	return &staticTokenProvider{
		token: cfg.SlackBotToken,
	}, nil
}

func hasTokenRotationConfig(cfg config) bool {
	return !isBlank(cfg.SlackClientId) &&
		!isBlank(cfg.SlackClientSecret) &&
		!isBlank(cfg.SlackRefreshToken)
}

func (p *rotatingTokenProvider) AccessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.shouldRefreshTokenLocked(false) {
		if err := p.refreshTokenLocked(ctx); err != nil {
			return "", err
		}
	}

	if isBlank(p.accessToken) {
		return "", errors.New(kErrorMissingSlackAuth)
	}

	return p.accessToken, nil
}

func (p *rotatingTokenProvider) ForceRefresh(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.refreshTokenLocked(ctx)
}

func (p *rotatingTokenProvider) shouldRefreshTokenLocked(force bool) bool {
	if force || isBlank(p.accessToken) {
		return true
	}
	if p.expiresAtUTC.IsZero() {
		return false
	}

	return time.Until(p.expiresAtUTC) <= kTokenRefreshLeadTime
}

func (p *rotatingTokenProvider) refreshTokenLocked(ctx context.Context) error {
	if isBlank(p.refreshToken) {
		return errors.New(kErrorMissingRefreshToken)
	}

	values := url.Values{}
	values.Set(kOAuthFieldClientId, p.cfg.SlackClientId)
	values.Set(kOAuthFieldClientSecret, p.cfg.SlackClientSecret)
	values.Set(kOAuthFieldGrantType, kOAuthRefreshToken)
	values.Set(kOAuthRefreshToken, p.refreshToken)

	resp, err := p.requestOAuthLocked(ctx, values)
	if err != nil {
		return err
	}
	if isBlank(resp.AccessToken) {
		return errors.New("oauth response invalid (missing_field: access_token)")
	}

	p.accessToken = resp.AccessToken
	if !isBlank(resp.RefreshToken) {
		p.refreshToken = resp.RefreshToken
	}
	if resp.ExpiresIn > 0 {
		p.expiresAtUTC = time.Now().UTC().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	if err := p.saveTokenStoreLocked(); err != nil {
		return err
	}

	log.Printf("token refreshed (expires_in_seconds: %d)", resp.ExpiresIn)
	return nil
}

func (p *rotatingTokenProvider) loadTokenStore() error {
	storePath := strings.TrimSpace(p.cfg.SlackTokenStorePath)
	if storePath == "" {
		return nil
	}

	bytes, err := os.ReadFile(storePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("token store read failed (path: %s, error: %w)", storePath, err)
	}

	var payload tokenStorePayload
	if err := json.Unmarshal(bytes, &payload); err != nil {
		return fmt.Errorf("token store parse failed (path: %s, error: %w)", storePath, err)
	}

	if !isBlank(payload.AccessToken) {
		p.accessToken = payload.AccessToken
	}
	if !isBlank(payload.RefreshToken) {
		p.refreshToken = payload.RefreshToken
	}
	if !isBlank(payload.ExpiresAtUTC) {
		expiresAtUTC, err := time.Parse(time.RFC3339, payload.ExpiresAtUTC)
		if err != nil {
			return fmt.Errorf("token store parse failed (field: expires_at_utc, error: %w)", err)
		}
		p.expiresAtUTC = expiresAtUTC.UTC()
	}

	log.Printf(
		"token store loaded (path: %s, has_access_token: %t, has_refresh_token: %t, has_expires_at_utc: %t)",
		storePath,
		!isBlank(p.accessToken),
		!isBlank(p.refreshToken),
		!p.expiresAtUTC.IsZero(),
	)
	return nil
}

func (p *rotatingTokenProvider) saveTokenStoreLocked() error {
	storePath := strings.TrimSpace(p.cfg.SlackTokenStorePath)
	if storePath == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		return fmt.Errorf("token store directory create failed (path: %s, error: %w)", storePath, err)
	}

	payload := tokenStorePayload{
		AccessToken:  p.accessToken,
		RefreshToken: p.refreshToken,
	}
	if !p.expiresAtUTC.IsZero() {
		payload.ExpiresAtUTC = p.expiresAtUTC.UTC().Format(time.RFC3339)
	}

	bytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("token store encode failed (error: %w)", err)
	}

	tempPath := storePath + ".tmp"
	if err := os.WriteFile(tempPath, append(bytes, '\n'), 0o600); err != nil {
		return fmt.Errorf("token store write failed (path: %s, error: %w)", tempPath, err)
	}
	if err := os.Rename(tempPath, storePath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("token store replace failed (path: %s, error: %w)", storePath, err)
	}

	log.Printf("token store saved (path: %s)", storePath)
	return nil
}

func (p *rotatingTokenProvider) requestOAuthLocked(ctx context.Context, values url.Values) (oauthResponse, error) {
	endpoint := strings.TrimRight(p.cfg.SlackApiUrl, "/") + kRouteSlackOAuthV2Access

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return oauthResponse{}, err
	}
	req.Header.Set("Content-Type", kFormContentType)

	resp, err := p.client.Do(req)
	if err != nil {
		return oauthResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return oauthResponse{}, err
	}

	var payload oauthResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return oauthResponse{}, fmt.Errorf("oauth response parse failed (error: %w)", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if payload.Error != "" {
			return oauthResponse{}, fmt.Errorf("oauth request failed (status: %d, error: %s)", resp.StatusCode, payload.Error)
		}
		return oauthResponse{}, fmt.Errorf("oauth request failed (status: %d)", resp.StatusCode)
	}
	if !payload.OK {
		return oauthResponse{}, fmt.Errorf("oauth request failed (error: %s)", payload.Error)
	}

	return payload, nil
}
