package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/exploreomni/omni-cli/internal/config"
)

func TestDo_SetsHeaders(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.ResolvedConfig{
		Token:   "test-token",
		BaseURL: srv.URL,
	}

	resp, err := Do(cfg, "GET", "/test", nil)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	resp.Body.Close()

	if got := gotHeaders.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer test-token")
	}
	if got := gotHeaders.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got := gotHeaders.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want %q", got, "application/json")
	}
}

func TestDo_MethodAndPath(t *testing.T) {
	tests := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/resources"},
		{"POST", "/api/v1/resources"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			var gotMethod, gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			cfg := &config.ResolvedConfig{
				Token:   "tok",
				BaseURL: srv.URL,
			}

			resp, err := Do(cfg, tt.method, tt.path, nil)
			if err != nil {
				t.Fatalf("Do returned error: %v", err)
			}
			resp.Body.Close()

			if gotMethod != tt.method {
				t.Errorf("method = %q, want %q", gotMethod, tt.method)
			}
			if gotPath != tt.path {
				t.Errorf("path = %q, want %q", gotPath, tt.path)
			}
		})
	}
}

func TestDo_SendsBody(t *testing.T) {
	body := []byte(`{"key":"value"}`)
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.ResolvedConfig{
		Token:   "tok",
		BaseURL: srv.URL,
	}

	resp, err := Do(cfg, "POST", "/api/v1/test", body)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	resp.Body.Close()

	if string(gotBody) != string(body) {
		t.Errorf("body = %q, want %q", gotBody, body)
	}
}

func TestDo_NilBody(t *testing.T) {
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.ResolvedConfig{
		Token:   "tok",
		BaseURL: srv.URL,
	}

	resp, err := Do(cfg, "GET", "/api/v1/test", nil)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	resp.Body.Close()

	if len(gotBody) != 0 {
		t.Errorf("expected empty body, got %q", gotBody)
	}
}

func TestDo_BaseURLTrailingSlash(t *testing.T) {
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.ResolvedConfig{
		Token:   "tok",
		BaseURL: srv.URL + "/",
	}

	resp, err := Do(cfg, "GET", "/api/v1/test", nil)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	resp.Body.Close()

	if gotPath != "/api/v1/test" {
		t.Errorf("path = %q, want %q (possible double slash)", gotPath, "/api/v1/test")
	}
}
