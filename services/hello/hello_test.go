package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPingHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/ping", nil)
	rec := httptest.NewRecorder()
	pingHandler(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status code: got %d, want %d", got, want)
	}
	if got, want := rec.Body.String(), `"status":"ok"`; !strings.Contains(got, want) {
		t.Fatalf("response body: got %q, want contains %q", got, want)
	}
}
