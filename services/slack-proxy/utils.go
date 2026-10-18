package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
)

const (
	kBearerPrefix         = "Bearer "
	kMaximumRequestBytes  = 1 << 20
	kMaximumResponseBytes = 8 << 20
)

var errBodyTooLarge = errors.New("body exceeds size limit")

func isBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}

func getEnvTrimmed(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func getEnvOrDefault(key string, defaultValue string) string {
	value := getEnvTrimmed(key)
	if value == "" {
		return defaultValue
	}

	return value
}

func formatBearerAuth(token string) string {
	return kBearerPrefix + token
}

func loadDotEnvIfExists(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		value = strings.TrimSpace(value)
		value = strings.Trim(value, "\"")

		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("env set failed (key: %s, error: %w)", key, err)
		}
	}

	return scanner.Err()
}

func readLimitedBody(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errBodyTooLarge
	}

	return body, nil
}

func readRequestBody(r *http.Request) ([]byte, error) {
	if r.Method != http.MethodPost {
		return nil, nil
	}

	return readLimitedBody(r.Body, kMaximumRequestBytes)
}

func errorReason(err error) string {
	if errors.Is(err, errBodyTooLarge) {
		return "body_too_large"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}

	var networkError net.Error
	if errors.As(err, &networkError) {
		if networkError.Timeout() {
			return "timeout"
		}

		return "network_error"
	}

	return "operation_failed"
}

func writeJson(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", kJsonContentType)
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, statusCode int, errorCode string) {
	writeJson(w, statusCode, map[string]string{
		"error": errorCode,
	})
}

func writeMethodNotAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, kErrorMethodNotAllowed)
}
