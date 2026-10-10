package rendezvousclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/rendezvous"
	"nhooyr.io/websocket"
)

const (
	maxClientWireBytes  = 160 << 10
	maxPresenceSnapshot = 256
)

// PresenceConfig describes one foreground Rendezvous registration. Topic and
// PresenceID must already be opaque values derived by peerdiscovery.
type PresenceConfig struct {
	ServiceURL            string
	Origin                string
	AllowInsecureLoopback bool
	Topic                 string
	PresenceID            string
	Role                  rendezvous.Role
}

// PresenceConnection is one registered WebSocket. Reconnect policy and Peer
// transport signaling are intentionally owned by the higher-level adapter.
type PresenceConnection struct {
	conn       *websocket.Conn
	writeMu    sync.Mutex
	topic      string
	presenceID string
}

// DialPresence registers with a fresh challenge identity and returns the
// service's current opaque snapshot. The durable Peer identity never leaves
// the device through this protocol step.
func DialPresence(ctx context.Context, cfg PresenceConfig) (*PresenceConnection, []rendezvous.Presence, error) {
	endpoint, err := ParseEndpoint(cfg.ServiceURL, cfg.AllowInsecureLoopback)
	if err != nil {
		return nil, nil, err
	}
	if err := validateOpaqueID(cfg.Topic, 32); err != nil {
		return nil, nil, fmt.Errorf("rendezvous client: invalid topic: %w", err)
	}
	if err := validateOpaqueID(cfg.PresenceID, 32); err != nil {
		return nil, nil, fmt.Errorf("rendezvous client: invalid presence id: %w", err)
	}
	if cfg.Role != rendezvous.RoleHost && cfg.Role != rendezvous.RoleMember {
		return nil, nil, errors.New("rendezvous client: invalid role")
	}
	header := make(http.Header)
	if cfg.Origin != "" {
		header.Set("Origin", cfg.Origin)
	}
	conn, response, err := websocket.Dial(ctx, endpoint.WebSocketURL, &websocket.DialOptions{
		Subprotocols: []string{rendezvous.Subprotocol}, HTTPHeader: header,
	})
	if err != nil {
		if response != nil {
			return nil, nil, fmt.Errorf("rendezvous client: dial status %d: %w", response.StatusCode, err)
		}
		return nil, nil, fmt.Errorf("rendezvous client: dial: %w", err)
	}
	connection := &PresenceConnection{conn: conn, topic: cfg.Topic, presenceID: cfg.PresenceID}
	fail := func(err error) (*PresenceConnection, []rendezvous.Presence, error) {
		conn.CloseNow()
		return nil, nil, err
	}
	conn.SetReadLimit(maxClientWireBytes)
	if conn.Subprotocol() != rendezvous.Subprotocol {
		return fail(errors.New("rendezvous client: server did not negotiate the v1 subprotocol"))
	}

	challenge, err := connection.ReadEvent(ctx)
	if err != nil {
		return fail(fmt.Errorf("rendezvous client: read challenge: %w", err))
	}
	if challenge.Kind != rendezvous.KindChallenge || validateOpaqueID(challenge.Challenge, 32) != nil || challenge.ExpiresAt == 0 {
		return fail(errors.New("rendezvous client: invalid challenge"))
	}
	ephemeralIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		return fail(fmt.Errorf("rendezvous client: create challenge identity: %w", err))
	}
	registration, err := rendezvous.SignRegistration(ephemeralIdentity, challenge.Challenge, cfg.Topic, cfg.PresenceID, cfg.Role)
	if err != nil {
		return fail(err)
	}
	if err := connection.writeJSON(ctx, registration); err != nil {
		return fail(fmt.Errorf("rendezvous client: write registration: %w", err))
	}
	registered, err := connection.ReadEvent(ctx)
	if err != nil {
		return fail(fmt.Errorf("rendezvous client: read registration result: %w", err))
	}
	if registered.Kind == rendezvous.KindError {
		switch registered.Code {
		case rendezvous.CodeUnauthorized:
			return fail(fmt.Errorf("%w: registration rejected", ErrAuthentication))
		default:
			return fail(fmt.Errorf("%w: registration rejected: %s", ErrServiceUnavailable, registered.Code))
		}
	}
	if registered.Kind != rendezvous.KindRegistered {
		return fail(errors.New("rendezvous client: registration was not acknowledged"))
	}
	snapshot, err := validatePresenceSnapshot(registered.Presence, cfg.PresenceID)
	if err != nil {
		return fail(err)
	}
	return connection, snapshot, nil
}

// ReadEvent reads and validates one bounded server event.
func (c *PresenceConnection) ReadEvent(ctx context.Context) (rendezvous.EventMessage, error) {
	if c == nil || c.conn == nil {
		return rendezvous.EventMessage{}, errors.New("rendezvous client: connection is closed")
	}
	messageType, payload, err := c.conn.Read(ctx)
	if err != nil {
		return rendezvous.EventMessage{}, err
	}
	if messageType != websocket.MessageText || len(payload) > maxClientWireBytes {
		return rendezvous.EventMessage{}, errors.New("rendezvous client: invalid event framing")
	}
	var event rendezvous.EventMessage
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return rendezvous.EventMessage{}, errors.New("rendezvous client: invalid event envelope")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return rendezvous.EventMessage{}, errors.New("rendezvous client: invalid trailing event data")
	}
	if event.Version != rendezvous.Version || !validEventKind(event.Kind) {
		return rendezvous.EventMessage{}, errors.New("rendezvous client: unsupported event")
	}
	return event, nil
}

// CloseNow immediately releases the socket. Higher-level lifecycle code owns
// graceful shutdown and reconnect timing.
func (c *PresenceConnection) CloseNow() {
	if c != nil && c.conn != nil {
		c.conn.CloseNow()
	}
}

// Publish sends opaque application ciphertext to one presence. The returned
// random message ID is used to correlate the later ack without exposing any
// application routing data.
func (c *PresenceConnection) Publish(ctx context.Context, to string, payload []byte) (string, error) {
	if c == nil || c.conn == nil || validateOpaqueID(to, 32) != nil || to == c.presenceID ||
		len(payload) == 0 || len(payload) > rendezvous.MaxPayloadBytes {
		return "", errors.New("rendezvous client: invalid publish")
	}
	messageID, err := newSignalMessageID()
	if err != nil {
		return "", fmt.Errorf("rendezvous client: create message id: %w", err)
	}
	if err := c.publishWithID(ctx, to, messageID, payload); err != nil {
		return "", err
	}
	return messageID, nil
}

func (c *PresenceConnection) publishWithID(ctx context.Context, to, messageID string, payload []byte) error {
	if c == nil || c.conn == nil || validateOpaqueID(to, 32) != nil || to == c.presenceID ||
		validateOpaqueID(messageID, 16) != nil || len(payload) == 0 || len(payload) > rendezvous.MaxPayloadBytes {
		return errors.New("rendezvous client: invalid publish")
	}
	request := rendezvous.PublishMessage{
		Version: rendezvous.Version, Kind: rendezvous.KindPublish, MessageID: messageID,
		To: to, Payload: base64.RawURLEncoding.EncodeToString(payload),
	}
	if err := c.writeJSON(ctx, request); err != nil {
		return fmt.Errorf("%w: publish: %v", ErrServiceUnavailable, err)
	}
	return nil
}

func (c *PresenceConnection) Topic() string {
	if c == nil {
		return ""
	}
	return c.topic
}

func (c *PresenceConnection) PresenceID() string {
	if c == nil {
		return ""
	}
	return c.presenceID
}

func (c *PresenceConnection) writeJSON(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, payload)
}

func validatePresenceSnapshot(snapshot []rendezvous.Presence, localPresenceID string) ([]rendezvous.Presence, error) {
	if len(snapshot) > maxPresenceSnapshot {
		return nil, errors.New("rendezvous client: presence snapshot exceeds limit")
	}
	seen := make(map[string]struct{}, len(snapshot))
	out := append([]rendezvous.Presence(nil), snapshot...)
	for _, presence := range out {
		if presence.PresenceID == localPresenceID || validateOpaqueID(presence.PresenceID, 32) != nil ||
			(presence.Role != rendezvous.RoleHost && presence.Role != rendezvous.RoleMember) {
			return nil, errors.New("rendezvous client: invalid presence snapshot")
		}
		if _, duplicate := seen[presence.PresenceID]; duplicate {
			return nil, errors.New("rendezvous client: duplicate presence snapshot entry")
		}
		seen[presence.PresenceID] = struct{}{}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PresenceID < out[j].PresenceID })
	return out, nil
}

func validateOpaqueID(value string, size int) error {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) != size || base64.RawURLEncoding.EncodeToString(raw) != value {
		return errors.New("expected canonical base64url value")
	}
	return nil
}

func validEventKind(kind string) bool {
	switch kind {
	case rendezvous.KindChallenge, rendezvous.KindRegistered, rendezvous.KindPresence,
		rendezvous.KindSignal, rendezvous.KindAck, rendezvous.KindError:
		return true
	default:
		return false
	}
}
