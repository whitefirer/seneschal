package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func reqWithOrigin(origin string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://example.com/api/ws", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func TestCheckOrigin(t *testing.T) {
	cfg := &ServerConfig{AllowedOrigins: []string{
		"http://localhost:8888",
		"https://app.example.com",
	}}
	check := cfg.CheckOrigin()

	cases := []struct {
		name   string
		origin string
		want   bool
	}{
		{"exact match", "http://localhost:8888", true},
		{"exact match https", "https://app.example.com", true},
		{"different port rejected", "http://localhost:9999", false},
		{"different scheme rejected", "https://localhost:8888", false},
		{"subdomain rejected", "http://evil.localhost:8888", false},
		{"empty origin rejected when list configured", "", false},
		{"trailing slash rejected", "http://localhost:8888/", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := check(reqWithOrigin(tc.origin)); got != tc.want {
				t.Errorf("origin %q: got %v, want %v", tc.origin, got, tc.want)
			}
		})
	}
}

func TestCheckOrigin_EmptyListAllowsAll(t *testing.T) {
	cfg := &ServerConfig{}
	check := cfg.CheckOrigin()
	if !check(reqWithOrigin("")) {
		t.Error("empty origin should be allowed when no whitelist is configured")
	}
	if !check(reqWithOrigin("http://anything.example")) {
		t.Error("any origin should be allowed when no whitelist is configured")
	}
}

func TestPortFlag(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"space form", []string{"srv", "--port", "7777"}, "7777"},
		{"equals form", []string{"srv", "--port=9999"}, "9999"},
		{"not set", []string{"srv"}, ""},
		{"missing value", []string{"srv", "--port"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Args = tc.args
			if got := PortFlag(); got != tc.want {
				t.Errorf("args %v: got %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestHostFlag(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"space form", []string{"srv", "--host", "0.0.0.0"}, "0.0.0.0"},
		{"equals form", []string{"srv", "--host=0.0.0.0"}, "0.0.0.0"},
		{"not set", []string{"srv"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Args = tc.args
			if got := HostFlag(); got != tc.want {
				t.Errorf("args %v: got %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestCORSMiddleware(t *testing.T) {
	cfg := &ServerConfig{AllowedOrigins: []string{"http://localhost:8888"}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := cfg.CORSMiddleware(next)

	t.Run("whitelisted origin gets ACAO", func(t *testing.T) {
		r := reqWithOrigin("http://localhost:8888")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("status=%d", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:8888" {
			t.Errorf("ACAO=%q", got)
		}
	})

	t.Run("non-whitelisted origin gets no ACAO", func(t *testing.T) {
		r := reqWithOrigin("http://evil.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("status=%d", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("ACAO=%q, want empty", got)
		}
	})

	t.Run("request without origin passes through", func(t *testing.T) {
		r := reqWithOrigin("")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("status=%d", rec.Code)
		}
	})

	t.Run("preflight whitelisted", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodOptions, "http://example.com/api/workflows", nil)
		r.Header.Set("Origin", "http://localhost:8888")
		r.Header.Set("Access-Control-Request-Method", "PUT")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status=%d", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "PUT") {
			t.Errorf("Allow-Methods=%q", got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
			t.Errorf("Allow-Headers=%q", got)
		}
	})

	t.Run("preflight from disallowed origin rejected", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodOptions, "http://example.com/api/workflows", nil)
		r.Header.Set("Origin", "http://evil.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status=%d, want 403", rec.Code)
		}
	})
}

func TestAuthMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("no token configured passes through", func(t *testing.T) {
		h := (&ServerConfig{}).AuthMiddleware(next)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflows", nil))
		if rec.Code != 200 {
			t.Fatalf("status=%d", rec.Code)
		}
	})

	cfg := &ServerConfig{AuthToken: "secret-token"}
	h := cfg.AuthMiddleware(next)

	t.Run("missing header rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflows", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401", rec.Code)
		}
	})

	t.Run("wrong token rejected", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/workflows", nil)
		r.Header.Set("Authorization", "Bearer wrong")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401", rec.Code)
		}
	})

	t.Run("correct bearer token accepted", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/workflows", nil)
		r.Header.Set("Authorization", "Bearer secret-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("status=%d", rec.Code)
		}
	})

	t.Run("ws query token accepted", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/ws?token=secret-token", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("status=%d", rec.Code)
		}
	})

	t.Run("ws without token rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ws", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401", rec.Code)
		}
	})

	t.Run("non-api path not gated", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/index.html", nil))
		if rec.Code != 200 {
			t.Fatalf("status=%d", rec.Code)
		}
	})
}
