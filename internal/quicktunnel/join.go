package quicktunnel

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/google/uuid"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	// PeerJoinPath is the accountless invitation redemption endpoint.
	PeerJoinPath = "/peer/v1/join"

	joinVersion             = 1
	joinRequestBodyLimit    = 512 << 10
	joinResponseBodyLimit   = 8 << 20
	joinBootstrapPlainLimit = 4 << 20
	joinKeyInfo             = "atterm-quick-tunnel-join-v1"
)

var ErrJoinRejected = errors.New("quicktunnel: join rejected")

// JoinBootstrap contains only signed governance tokens and recipient-bound
// epoch envelopes. Callers must verify and persist all fields before reporting
// a successful join.
type JoinBootstrap struct {
	GenesisToken    string   `json:"genesis"`
	MembershipToken string   `json:"membership"`
	Memberships     []string `json:"memberships"`
	Revocations     []string `json:"revocations,omitempty"`
	EpochRotations  []string `json:"epoch_rotations"`
	EpochEnvelopes  []string `json:"epoch_envelopes"`
	// ConnectionBundle is a newly signed, ticketless route bundle. Clients
	// persist this replacement instead of the first-join bundle so the pairing
	// secret is erased with the request.
	ConnectionBundle string `json:"connection_bundle"`
}

// JoinHostConfig supplies the invitation secret lookup and the issuer's
// atomic redemption operation. LookupSecret must return exactly 32 bytes.
type JoinHostConfig struct {
	LookupSecret func(context.Context, string) ([]byte, error)
	Redeem       func(context.Context, string) (JoinBootstrap, error)
}

// JoinClientConfig contains the new device identity and a first-join bundle.
// URL and AllowInsecure exist only for loopback tests; production callers use
// the signed Quick Tunnel route from BundleToken.
type JoinClientConfig struct {
	URL               string
	AllowInsecure     bool
	BundleToken       string
	Identity          *peercrypto.Identity
	WrappingPublicKey []byte
	HTTPClient        *http.Client
}

type joinEnvelope struct {
	Version    int    `json:"v"`
	InviteID   string `json:"invite_id"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type joinRequestPayload struct {
	Version     int    `json:"v"`
	JoinRequest string `json:"join_request"`
}

func (h *PeerHandler) serveJoin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.cfg.Join == nil || !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		http.Error(w, "join unavailable", http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, joinRequestBodyLimit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "join rejected", http.StatusForbidden)
		return
	}
	var envelope joinEnvelope
	if decodeStrictJSON(body, &envelope) != nil || envelope.Version != joinVersion || !validJoinInviteID(envelope.InviteID) {
		http.Error(w, "join rejected", http.StatusForbidden)
		return
	}
	secret, err := h.cfg.Join.LookupSecret(r.Context(), envelope.InviteID)
	if err != nil || len(secret) != chacha20poly1305.KeySize {
		http.Error(w, "join rejected", http.StatusForbidden)
		return
	}
	plaintext, err := openJoinEnvelope(envelope, secret, "request")
	if err != nil {
		http.Error(w, "join rejected", http.StatusForbidden)
		return
	}
	var payload joinRequestPayload
	if decodeStrictJSON(plaintext, &payload) != nil || payload.Version != joinVersion || payload.JoinRequest == "" {
		http.Error(w, "join rejected", http.StatusForbidden)
		return
	}
	bootstrap, err := h.cfg.Join.Redeem(r.Context(), payload.JoinRequest)
	if err != nil {
		http.Error(w, "join rejected", http.StatusForbidden)
		return
	}
	bootstrapJSON, err := json.Marshal(bootstrap)
	if err != nil || len(bootstrapJSON) > joinBootstrapPlainLimit {
		http.Error(w, "join rejected", http.StatusForbidden)
		return
	}
	response, err := sealJoinEnvelope(envelope.InviteID, secret, "response", bootstrapJSON)
	if err != nil {
		http.Error(w, "join rejected", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		return
	}
}

// Join redeems a first-join ConnectionBundle without exposing the embedded
// invitation or signed JoinRequest to the Quick Tunnel provider.
func Join(ctx context.Context, cfg JoinClientConfig) (JoinBootstrap, error) {
	if cfg.Identity == nil {
		return JoinBootstrap{}, fmt.Errorf("%w: missing identity", ErrJoinRejected)
	}
	if _, err := peercrypto.ValidateWrappingPublicKey(cfg.WrappingPublicKey); err != nil {
		return JoinBootstrap{}, fmt.Errorf("%w: wrapping identity", ErrJoinRejected)
	}
	bundle, err := peerproto.VerifyConnectionBundle(strings.TrimSpace(cfg.BundleToken), time.Now())
	if err != nil || bundle.Ticket == nil {
		return JoinBootstrap{}, fmt.Errorf("%w: first-join bundle", ErrJoinRejected)
	}
	secret, err := base64.RawURLEncoding.Strict().DecodeString(bundle.Ticket.PairingSecret)
	if err != nil || len(secret) != chacha20poly1305.KeySize {
		return JoinBootstrap{}, fmt.Errorf("%w: pairing secret", ErrJoinRejected)
	}
	joinRequest, err := peerproto.NewJoinRequest(cfg.Identity, cfg.WrappingPublicKey, bundle.Document.Ticket, time.Now())
	if err != nil {
		return JoinBootstrap{}, fmt.Errorf("create join request: %w", err)
	}
	payload, err := json.Marshal(joinRequestPayload{Version: joinVersion, JoinRequest: joinRequest})
	if err != nil {
		return JoinBootstrap{}, err
	}
	envelope, err := sealJoinEnvelope(bundle.Ticket.InviteID, secret, "request", payload)
	if err != nil {
		return JoinBootstrap{}, err
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return JoinBootstrap{}, err
	}
	endpoint, err := joinEndpoint(bundle, cfg.URL, cfg.AllowInsecure)
	if err != nil {
		return JoinBootstrap{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return JoinBootstrap{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	if cfg.HTTPClient != nil {
		*client = *cfg.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("quicktunnel: join redirect rejected")
	}
	response, err := client.Do(request)
	if err != nil {
		return JoinBootstrap{}, fmt.Errorf("Quick Tunnel join request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return JoinBootstrap{}, ErrJoinRejected
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, joinResponseBodyLimit+1))
	if err != nil || len(responseBody) > joinResponseBodyLimit {
		return JoinBootstrap{}, ErrJoinRejected
	}
	var sealed joinEnvelope
	if decodeStrictJSON(responseBody, &sealed) != nil || sealed.InviteID != bundle.Ticket.InviteID {
		return JoinBootstrap{}, ErrJoinRejected
	}
	plaintext, err := openJoinEnvelope(sealed, secret, "response")
	if err != nil || len(plaintext) > joinBootstrapPlainLimit {
		return JoinBootstrap{}, ErrJoinRejected
	}
	var bootstrap JoinBootstrap
	if decodeStrictJSON(plaintext, &bootstrap) != nil {
		return JoinBootstrap{}, ErrJoinRejected
	}
	return bootstrap, nil
}

func joinEndpoint(bundle peerproto.VerifiedConnectionBundle, override string, allowInsecure bool) (string, error) {
	base := ""
	if override != "" {
		if !allowInsecure {
			return "", errors.New("quicktunnel: join URL override is test-only")
		}
		base = override
	} else {
		for _, route := range bundle.Document.Routes {
			if route.Kind == peerproto.RouteQuickTunnel {
				base = route.URL
				break
			}
		}
		if base == "" {
			for _, route := range bundle.Document.Routes {
				if route.Kind == peerproto.RouteManualLAN {
					base = route.URL
					break
				}
			}
		}
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" {
		return "", errors.New("quicktunnel: invalid join route")
	}
	if parsed.Scheme != "https" {
		manualLAN := false
		for _, route := range bundle.Document.Routes {
			manualLAN = manualLAN || route.Kind == peerproto.RouteManualLAN && route.URL == base
		}
		if parsed.Scheme != "http" || !manualLAN && (!allowInsecure || !isLoopbackHost(parsed.Hostname())) {
			return "", errors.New("quicktunnel: insecure join route")
		}
	}
	parsed.Path = PeerJoinPath
	parsed.RawPath = ""
	return parsed.String(), nil
}

func sealJoinEnvelope(inviteID string, secret []byte, direction string, plaintext []byte) (joinEnvelope, error) {
	if !validJoinInviteID(inviteID) || len(secret) != chacha20poly1305.KeySize || len(plaintext) == 0 {
		return joinEnvelope{}, ErrJoinRejected
	}
	key, err := deriveJoinKey(secret, inviteID, direction)
	if err != nil {
		return joinEnvelope{}, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return joinEnvelope{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return joinEnvelope{}, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, joinAAD(inviteID, direction))
	return joinEnvelope{
		Version: joinVersion, InviteID: inviteID,
		Nonce: base64.RawURLEncoding.EncodeToString(nonce), Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext),
	}, nil
}

func openJoinEnvelope(envelope joinEnvelope, secret []byte, direction string) ([]byte, error) {
	if envelope.Version != joinVersion || !validJoinInviteID(envelope.InviteID) || len(secret) != chacha20poly1305.KeySize {
		return nil, ErrJoinRejected
	}
	key, err := deriveJoinKey(secret, envelope.InviteID, direction)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce, err := decodeJoinField(envelope.Nonce, aead.NonceSize())
	if err != nil {
		return nil, err
	}
	ciphertext, err := decodeJoinField(envelope.Ciphertext, -1)
	if err != nil || len(ciphertext) < aead.Overhead() {
		return nil, ErrJoinRejected
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, joinAAD(envelope.InviteID, direction))
	if err != nil {
		return nil, ErrJoinRejected
	}
	return plaintext, nil
}

func deriveJoinKey(secret []byte, inviteID, direction string) ([]byte, error) {
	if direction != "request" && direction != "response" {
		return nil, ErrJoinRejected
	}
	reader := hkdf.New(sha256.New, secret, []byte(inviteID), []byte(joinKeyInfo+"\x00"+direction))
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

func joinAAD(inviteID, direction string) []byte {
	return []byte(joinKeyInfo + "\x00" + direction + "\x00" + inviteID)
}

func decodeJoinField(value string, size int) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != value || size >= 0 && len(decoded) != size {
		return nil, ErrJoinRejected
	}
	return decoded, nil
}

func validJoinInviteID(value string) bool {
	return uuid.Validate(value) == nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
