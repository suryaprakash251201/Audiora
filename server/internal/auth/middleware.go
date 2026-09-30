package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

type ctxKey int

const claimsKey ctxKey = iota

// Authenticator carries the shared dependencies the middleware needs.
type Authenticator struct {
	Issuer *TokenIssuer
	Store  *Store
}

// Middleware rejects any request without a valid bearer token.
//
// The mobile app has no cookie jar to rely on, so the whole API is
// bearer-token only rather than supporting both schemes.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			unauthorized(w, "missing bearer token")
			return
		}
		claims, err := a.Issuer.Parse(strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			unauthorized(w, "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin wraps Authenticator.Middleware and additionally insists the
// caller is an administrator.
func (a *Authenticator) RequireAdmin(next http.Handler) http.Handler {
	return a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ClaimsFrom(r.Context()).Admin {
			http.Error(w, "admin privileges required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// MediaMiddleware authorises routes that the browser cannot attach a header
// to: <audio src> for streaming and the WebSocket constructor for sync.
//
// It accepts a bearer token as normal, and falls back to a "t" query
// parameter. The fallback is a deliberate tradeoff: there is no other way to
// authenticate those two requests from JavaScript. The token in the URL can
// end up in proxy access logs, which is why Caddy is configured not to log
// query strings and why this is not used for any other route.
func (a *Authenticator) MediaMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
			token = strings.TrimPrefix(header, "Bearer ")
		} else if q := r.URL.Query().Get("t"); q != "" {
			token = q
		}
		if token == "" {
			unauthorized(w, "missing access token")
			return
		}
		claims, err := a.Issuer.Parse(token)
		if err != nil {
			unauthorized(w, "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ClaimsFrom returns the verified claims for a request that has passed
// through Middleware.
func ClaimsFrom(ctx context.Context) *Claims {
	claims, _ := ctx.Value(claimsKey).(*Claims)
	return claims
}

// UserIDFrom is a convenience wrapper for handlers that only need the id.
func UserIDFrom(ctx context.Context) int64 {
	if c := ClaimsFrom(ctx); c != nil {
		return c.UserID
	}
	return 0
}

func unauthorized(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	// The body is a plain object rather than middleware.JSON to avoid an
	// import cycle with the api package.
	w.Write([]byte(`{"error":"unauthorized","message":` + quote(reason) + `}`))
}

// RealIP and NoCache are small helpers reused by the API package so that
// every response gets the same request-id and no-store treatment.
func NoCache(next http.Handler) http.Handler {
	return middleware.NoCache(next)
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
