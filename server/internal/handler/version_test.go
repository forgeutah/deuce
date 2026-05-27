package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVersion_ReturnsInjectedVersionAsJSON(t *testing.T) {
	h := Version("v1.2.3")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type: want application/json, got %q", ct)
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if got := payload["version"]; got != "v1.2.3" {
		t.Errorf("version field: want v1.2.3, got %q", got)
	}
}

func TestVersion_DevDefaultWorks(t *testing.T) {
	h := Version("dev")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200 for dev, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"version":"dev"`) {
		t.Errorf("body should carry dev version: %q", rec.Body.String())
	}
}

func TestVersion_EmptyStringDoesNotPanic(t *testing.T) {
	h := Version("")
	rec := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Version(\"\") should not panic: %v", r)
		}
	}()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200 for empty version, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"version":""`) {
		t.Errorf("body should explicitly carry empty version: %q", rec.Body.String())
	}
}
