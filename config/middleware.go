package config

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// CORSMiddleware adds CORS response headers for whitelisted origins and
// answers OPTIONS preflight requests. Origins not in AllowedOrigins get no
// Access-Control-* headers (browsers then block the response client-side);
// requests without an Origin header (curl et al.) pass through untouched.
// Mount it outermost so preflight requests reach it before auth.
func (c *ServerConfig) CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := origin != "" && c.OriginAllowed(origin)
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			if origin != "" && !allowed {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			if allowed {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "3600")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AuthMiddleware enforces Bearer-token auth on /api/ endpoints when AuthToken
// is configured. When AuthToken is empty it is a no-op (loopback-only dev
// mode; see the AuthToken field doc).
//
// /api/ws additionally accepts a "?token=" query parameter because browser
// WebSocket clients cannot set request headers. Static (non-/api/) assets are
// not gated: they are read-only UI files, and locking them would make the web
// UI unreachable from a browser, which cannot attach Authorization headers to
// navigation requests.
func (c *ServerConfig) AuthMiddleware(next http.Handler) http.Handler {
	if c.AuthToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		token := ""
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
		if token == "" && r.URL.Path == "/api/ws" {
			token = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(c.AuthToken)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"success":false,"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
