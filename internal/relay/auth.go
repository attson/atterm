package relay

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

type authScope uint8

const (
	authNone authScope = iota
	authRead
	authWrite
)

// authorize accepts an Authorization: Bearer <token> header, a REST/bootstrap
// ?token=<token> query parameter, or an auth WebSocket subprotocol.
func authorize(r *http.Request, expected string) bool {
	return authorizeWithScope(r, expected, nil) == authWrite
}

func authorizeClient(r *http.Request, expected string, readOnlyTokens []string) authScope {
	return authorizeWithScopeAndHashes(r, expected, readOnlyTokens, nil)
}

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

func tokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if q := r.URL.Query().Get("token"); q != "" {
		return q
	}
	return tokenFromSubprotocol(r)
}

func tokenFromRequestNoQuery(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return tokenFromSubprotocol(r)
}

func tokenFromSubprotocol(r *http.Request) string {
	if p := r.Header.Get("Sec-WebSocket-Protocol"); p != "" {
		// allow browsers that pass token via the subprotocol header
		for _, part := range strings.Split(p, ",") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "atterm-token.") {
				return strings.TrimPrefix(part, "atterm-token.")
			}
			if strings.HasPrefix(part, "atterm-token-b64.") {
				decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(part, "atterm-token-b64."))
				if err == nil {
					return string(decoded)
				}
			}
		}
	}
	return ""
}
