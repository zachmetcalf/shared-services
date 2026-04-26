package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const kBearerPrefix = "Bearer "

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
			return fmt.Errorf("set env failed (key: %s): %w", key, err)
		}
	}

	return scanner.Err()
}

func readRequestBody(r *http.Request, method string) ([]byte, error) {
	if method != http.MethodPost {
		return nil, nil
	}

	return io.ReadAll(r.Body)
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
