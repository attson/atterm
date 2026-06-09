package relay

// TODO(task-1.9): This file holds the legacy shared-token authorize* family,
// kept temporarily so callers in server.go / identity.go / permissions.go /
// client_conn.go / web_push_http.go still compile. Task 1.9 deletes this file
// after rewiring the protected routes to use the new requireSession middleware.

import (
	"crypto/subtle"
	"net/http"
)

type authScope uint8

const (
	authNone authScope = iota
	authRead
	authWrite
)

func authorizeWithScope(r *http.Request, expected string, readOnlyTokens []string) authScope {
	return authorizeWithScopeAndHashes(r, expected, readOnlyTokens, nil)
}

func authorizeClientWithConfig(r *http.Request, cfg Config) authScope {
	return authorizeWithScopeAndHashes(r, cfg.Token, cfg.ReadOnlyTokens, cfg.ReadOnlyTokenHashes)
}

func authorizeClientWebSocketWithConfig(r *http.Request, cfg Config) authScope {
	if _, hasQueryToken := r.URL.Query()["token"]; hasQueryToken {
		return authNone
	}
	return authorizeWithScopeAndHashesFromToken(tokenFromRequestNoQuery(r), cfg.Token, cfg.ReadOnlyTokens, cfg.ReadOnlyTokenHashes)
}

func authorizeWithScopeAndHashes(r *http.Request, expected string, readOnlyTokens []string, readOnlyHashes []string) authScope {
	return authorizeWithScopeAndHashesFromToken(tokenFromRequest(r), expected, readOnlyTokens, readOnlyHashes)
}

func authorizeWithScopeAndHashesFromToken(got, expected string, readOnlyTokens []string, readOnlyHashes []string) authScope {
	if expected == "" && len(readOnlyTokens) == 0 && len(readOnlyHashes) == 0 {
		return authWrite // dev mode: no token configured
	}
	if got == "" {
		return authNone
	}
	if tokenEqual(got, expected) {
		return authWrite
	}
	for _, ro := range readOnlyTokens {
		if tokenEqual(got, ro) {
			return authRead
		}
	}
	for _, hash := range readOnlyHashes {
		if tokenMatchesHash(got, hash) {
			return authRead
		}
	}
	return authNone
}

func tokenEqual(got, expected string) bool {
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// tokenMatchesHash is declared in admin_config.go.

func tokenFromRequestNoQuery(r *http.Request) string {
	return tokenFromRequest(r)
}
