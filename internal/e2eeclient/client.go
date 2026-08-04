// Package e2eeclient is the Go SDK that desktop and CLI clients use to
// register, log in, and unlock their account_key against an atterm-relay
// running the OPAQUE-based E2EE auth flow added in M1a/M1b.
//
// The SDK owns the OPAQUE protocol round-trips (via
// github.com/bytemare/opaque) and speaks JSON to the relay's
// /api/opaque/* endpoints. Wire-format helpers live in
// internal/opaquesuite; the account_key wrap/unwrap crypto (Argon2id
// derivation + XChaCha20-Poly1305 AEAD) lives in internal/e2eecrypto.
//
// The relay never sees plaintext password, plaintext account_key, or the
// wrap key. See docs/superpowers/specs/2026-06-15-relay-e2ee-design.md §4.
package e2eeclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/opaquesuite"
	"github.com/bytemare/opaque"
)

// Server identity bound into the AKE transcript. Shared with the relay server
// and the browser WASM client via internal/opaquesuite.
const serverIdentity = opaquesuite.ServerIdentity

// AccountKeyWrap is the on-wire wrap envelope shared with the relay. The
// struct lives in internal/opaquesuite so the relay and this SDK cannot
// silently drift apart. Re-exported here as an alias so external callers can
// keep writing e2eeclient.AccountKeyWrap unchanged.
type AccountKeyWrap = opaquesuite.AccountKeyWrap

// Client speaks HTTP to a single relay. Construct one per relay base URL.
type Client struct {
	// BaseURL is the relay's HTTP(S) origin, e.g. "https://relay.example.com".
	// No trailing slash; the SDK appends paths starting with "/api/...".
	BaseURL string
	// HTTPClient is the underlying transport. Nil means http.DefaultClient.
	HTTPClient *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// LoginResult is the return value of Login. The session token is what
// goes in Authorization headers / cookies for follow-on requests.
// AccountKey is 32 random bytes used to derive per-session keys via HKDF
// (see spec §5). Email is echoed back so the caller can persist it.
type LoginResult struct {
	UserID          string
	SessionToken    string
	Email           string
	AccountKey      []byte // 32 bytes
	RealmID         string
	HomeInstanceURL string
}

// RegisterResult is the return value of Register.
type RegisterResult struct {
	UserID       string
	SessionToken string
	Email        string
	AccountKey   []byte // 32 bytes
	RealmID      string
}

// Register completes a fresh OPAQUE registration against the relay, mints
// a new account_key client-side, wraps it with the password-derived
// wrap_key, and uploads the envelope. claimToken is optional — supply the
// plaintext token emitted by `atterm-relay` bootstrap to also promote the
// new user to admin.
func (c *Client) Register(ctx context.Context, email, password, claimToken string) (*RegisterResult, error) {
	if c.BaseURL == "" {
		return nil, errors.New("e2eeclient: BaseURL is empty")
	}
	conf := defaultOpaqueConfig()
	cl, err := conf.Client()
	if err != nil {
		return nil, fmt.Errorf("opaque client: %w", err)
	}

	ke1 := cl.RegistrationInit([]byte(password))
	initBody, _ := json.Marshal(registerInitRequest{
		Email:          email,
		RegistrationKE: ke1.Serialize(),
	})
	var initResp registerInitResponse
	if err := c.do(ctx, "POST", "/api/auth/register/init", initBody, &initResp); err != nil {
		return nil, fmt.Errorf("register init: %w", err)
	}

	ke2, err := cl.Deserialize.RegistrationResponse(initResp.RegistrationResponse)
	if err != nil {
		return nil, fmt.Errorf("decode ke2: %w", err)
	}
	record, _ := cl.RegistrationFinalize(ke2, opaque.ClientRegistrationFinalizeOptions{
		ClientIdentity: []byte(email),
		ServerIdentity: []byte(serverIdentity),
	})

	// Generate a fresh 32-byte account_key and wrap it with a
	// password-derived key. The relay never sees plaintext bytes.
	accountKey := make([]byte, 32)
	if _, err := rand.Read(accountKey); err != nil {
		return nil, fmt.Errorf("rand account_key: %w", err)
	}
	wrap, err := e2eecrypto.WrapAccountKey(password, accountKey, e2eecrypto.DefaultKDFParams())
	if err != nil {
		return nil, fmt.Errorf("wrap account_key: %w", err)
	}

	finBody, _ := json.Marshal(registerFinalizeRequest{
		Email:              email,
		RegistrationRecord: record.Serialize(),
		AccountKeyWrap:     wrap,
		ClaimToken:         claimToken,
	})
	var finResp registerFinalizeResponse
	if err := c.do(ctx, "POST", "/api/auth/register/finalize", finBody, &finResp); err != nil {
		return nil, fmt.Errorf("register finalize: %w", err)
	}
	if finResp.SessionToken == "" || finResp.UserID == "" {
		return nil, errors.New("register finalize: empty session_token or user_id")
	}

	return &RegisterResult{
		UserID:       finResp.UserID,
		SessionToken: finResp.SessionToken,
		Email:        email,
		AccountKey:   accountKey,
		RealmID:      finResp.RealmID,
	}, nil
}

// Login completes a full OPAQUE login round-trip against the relay,
// fetches the wrapped account_key from the response, and unwraps it
// locally with the password-derived key.
func (c *Client) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	if c.BaseURL == "" {
		return nil, errors.New("e2eeclient: BaseURL is empty")
	}
	conf := defaultOpaqueConfig()
	cl, err := conf.Client()
	if err != nil {
		return nil, fmt.Errorf("opaque client: %w", err)
	}

	ke1 := cl.LoginInit([]byte(password))
	initBody, _ := json.Marshal(loginInitRequest{
		Email:   email,
		LoginKE: ke1.Serialize(),
	})
	var initResp loginInitResponse
	if err := c.do(ctx, "POST", "/api/auth/login/init", initBody, &initResp); err != nil {
		return nil, fmt.Errorf("login init: %w", err)
	}

	ke2, err := cl.Deserialize.KE2(initResp.LoginResponse)
	if err != nil {
		return nil, fmt.Errorf("decode ke2: %w", err)
	}
	ke3, _, err := cl.LoginFinish(ke2, opaque.ClientLoginFinishOptions{
		ClientIdentity: []byte(email),
		ServerIdentity: []byte(serverIdentity),
	})
	if err != nil {
		// Library-detected wrong password — server never finalizes.
		return nil, fmt.Errorf("login: invalid credentials")
	}

	finBody, _ := json.Marshal(loginFinalizeRequest{
		Email:     email,
		SessionID: initResp.SessionID,
		LoginKE3:  ke3.Serialize(),
	})
	var finResp loginFinalizeResponse
	if err := c.do(ctx, "POST", "/api/auth/login/finalize", finBody, &finResp); err != nil {
		return nil, fmt.Errorf("login finalize: %w", err)
	}
	if finResp.SessionToken == "" || finResp.UserID == "" {
		return nil, errors.New("login finalize: empty session_token or user_id")
	}

	accountKey, err := e2eecrypto.UnwrapAccountKey(password, finResp.AccountKeyWrap)
	if err != nil {
		return nil, fmt.Errorf("unwrap account_key: %w", err)
	}

	return &LoginResult{
		UserID:          finResp.UserID,
		SessionToken:    finResp.SessionToken,
		Email:           email,
		AccountKey:      accountKey,
		RealmID:         finResp.RealmID,
		HomeInstanceURL: finResp.HomeInstanceURL,
	}, nil
}

// ---- internal helpers ----

// defaultOpaqueConfig returns the shared OPAQUE suite. The relay server and the
// browser WASM client use the SAME internal/opaquesuite.Config(), so all three
// bytemare endpoints stay byte-identical (cross-client interop).
func defaultOpaqueConfig() *opaque.Configuration {
	return opaquesuite.Config()
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	base := strings.TrimRight(c.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: status=%d body=%s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) doAuthed(ctx context.Context, method, path string, body []byte, sessionToken string, out any) error {
	base := strings.TrimRight(c.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: status=%d body=%s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ---- wire types ----
//
// The struct definitions live in internal/opaquesuite/wire.go so this SDK
// and the atterm-relay server share exactly one source of truth. Unexported
// aliases keep the rest of this file (Register/Login bodies) unchanged. The
// prior in-file registerFinalizeResponse also silently dropped the IsAdmin
// field the relay returns; going through the shared struct now preserves it
// so future admin-aware flows can read reg.IsAdmin.

type (
	registerInitRequest      = opaquesuite.RegisterInitRequest
	registerInitResponse     = opaquesuite.RegisterInitResponse
	registerFinalizeRequest  = opaquesuite.RegisterFinalizeRequest
	registerFinalizeResponse = opaquesuite.RegisterFinalizeResponse
	loginInitRequest         = opaquesuite.LoginInitRequest
	loginInitResponse        = opaquesuite.LoginInitResponse
	loginFinalizeRequest     = opaquesuite.LoginFinalizeRequest
	loginFinalizeResponse    = opaquesuite.LoginFinalizeResponse
)
