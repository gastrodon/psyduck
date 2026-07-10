package server

import (
	"crypto/subtle"
	"net/http"
)

// Option configures a Server at construction. Options are applied before
// routes are wired, so they can influence which routes are protected.
type Option func(*Server)

// WithBasicAuth protects the plugin-management routes with HTTP Basic auth,
// using the same "user:pass" credential convention as stdlib's request
// transport (see stdlib/transport/http.go). Passing an empty user and pass
// leaves auth disabled — the zero value — so a caller can apply it
// unconditionally.
//
// Plugin registration runs `git clone` + `go build` on operator-supplied
// input, so it's the one part of the API that must not be open on an
// exposed bind; this is the gate for it.
func WithBasicAuth(user, pass string) Option {
	return func(s *Server) {
		s.authUser = user
		s.authPass = pass
	}
}

// authEnabled reports whether any credential was configured.
func (s *Server) authEnabled() bool {
	return s.authUser != "" || s.authPass != ""
}

// guard wraps h to require valid Basic credentials when auth is configured;
// with no credentials it is a passthrough, so unprotected routes and the
// auth-disabled case share one code path. Credential comparison is
// constant-time to avoid leaking the secret through timing.
func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc {
	if !s.authEnabled() {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(s.authUser)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(s.authPass)) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="psyduck", charset="UTF-8"`)
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		h(w, r)
	}
}
