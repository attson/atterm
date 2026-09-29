// Package rendezvouscontract provides a black-box compatibility suite for
// official and self-hosted Rendezvous deployments.
package rendezvouscontract

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/rendezvous"
	"github.com/attson/atterm/internal/rendezvousclient"
	"nhooyr.io/websocket"
)

const defaultTimeout = 15 * time.Second

type Config struct {
	URL                   string
	Origin                string
	AllowInsecureLoopback bool
	Timeout               time.Duration
}

// Report contains no routing identifiers or payload material and is safe to
// keep as deployment verification output.
type Report struct {
	ProtocolVersion   int  `json:"protocol_version"`
	LiveDelivery      bool `json:"live_delivery"`
	MailboxDelivery   bool `json:"mailbox_delivery"`
	RetryDeduplicated bool `json:"retry_deduplicated"`
	OversizeRejected  bool `json:"oversize_rejected"`
	MetricsPrivate    bool `json:"metrics_private"`
}

// Run verifies one deployed service only through its public HTTP/WebSocket
// contract. It does not depend on server state, storage, or handler internals.
func Run(parent context.Context, cfg Config) (Report, error) {
	endpoint, err := rendezvousclient.ParseEndpoint(cfg.URL, cfg.AllowInsecureLoopback)
	if err != nil {
		return Report{}, err
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if timeout < time.Second || timeout > 2*time.Minute {
		return Report{}, errors.New("rendezvous contract: timeout must be between 1s and 2m")
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	version, err := checkHealth(ctx, endpoint.HealthURL)
	if err != nil {
		return Report{}, err
	}
	report := Report{ProtocolVersion: version}
	topic, err := randomOpaqueID(32)
	if err != nil {
		return Report{}, err
	}
	hostPresence, err := randomOpaqueID(32)
	if err != nil {
		return Report{}, err
	}
	memberPresence, err := randomOpaqueID(32)
	if err != nil {
		return Report{}, err
	}
	hostIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		return Report{}, fmt.Errorf("rendezvous contract: create host identity: %w", err)
	}
	memberIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		return Report{}, fmt.Errorf("rendezvous contract: create member identity: %w", err)
	}
	host, err := dialRegistered(ctx, endpoint.WebSocketURL, cfg.Origin, topic, hostPresence, rendezvous.RoleHost, hostIdentity)
	if err != nil {
		return Report{}, fmt.Errorf("rendezvous contract: register host: %w", err)
	}
	defer host.CloseNow()
	member, err := dialRegistered(ctx, endpoint.WebSocketURL, cfg.Origin, topic, memberPresence, rendezvous.RoleMember, memberIdentity)
	if err != nil {
		return Report{}, fmt.Errorf("rendezvous contract: register member: %w", err)
	}
	defer member.CloseNow()
	if event, err := readUntilKind(ctx, host, rendezvous.KindPresence); err != nil ||
		event.Event != rendezvous.PresenceOnline || event.PresenceID != memberPresence {
		return Report{}, errors.New("rendezvous contract: online presence was not delivered")
	}

	livePayload, err := randomOpaqueID(32)
	if err != nil {
		return Report{}, err
	}
	liveMessageID, err := randomOpaqueID(16)
	if err != nil {
		return Report{}, err
	}
	if err := writeJSON(ctx, member, rendezvous.PublishMessage{
		Version: rendezvous.Version, Kind: rendezvous.KindPublish, MessageID: liveMessageID,
		To: hostPresence, Payload: livePayload,
	}); err != nil {
		return Report{}, err
	}
	signal, err := readUntilKind(ctx, host, rendezvous.KindSignal)
	if err != nil || signal.MessageID != liveMessageID || signal.From != memberPresence || signal.Payload != livePayload {
		return Report{}, errors.New("rendezvous contract: live signal mismatch")
	}
	ack, err := readUntilKind(ctx, member, rendezvous.KindAck)
	if err != nil || ack.MessageID != liveMessageID || ack.State != rendezvous.DeliveryDelivered {
		return Report{}, errors.New("rendezvous contract: live delivery acknowledgement mismatch")
	}
	report.LiveDelivery = true

	oversizeMessageID, err := randomOpaqueID(16)
	if err != nil {
		return Report{}, err
	}
	if err := writeJSON(ctx, member, rendezvous.PublishMessage{
		Version: rendezvous.Version, Kind: rendezvous.KindPublish, MessageID: oversizeMessageID,
		To: hostPresence, Payload: base64.RawURLEncoding.EncodeToString(make([]byte, rendezvous.MaxPayloadBytes+1)),
	}); err != nil {
		return Report{}, err
	}
	oversize, err := readUntilKind(ctx, member, rendezvous.KindError)
	if err != nil || oversize.MessageID != oversizeMessageID || oversize.Code != rendezvous.CodeMessageTooLarge {
		return Report{}, errors.New("rendezvous contract: oversize payload was not rejected")
	}
	report.OversizeRejected = true

	offlinePresence, err := randomOpaqueID(32)
	if err != nil {
		return Report{}, err
	}
	queuedMessageID, err := randomOpaqueID(16)
	if err != nil {
		return Report{}, err
	}
	queuedPayload, err := randomOpaqueID(32)
	if err != nil {
		return Report{}, err
	}
	publish := rendezvous.PublishMessage{
		Version: rendezvous.Version, Kind: rendezvous.KindPublish, MessageID: queuedMessageID,
		To: offlinePresence, Payload: queuedPayload,
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := writeJSON(ctx, member, publish); err != nil {
			return Report{}, err
		}
		ack, err := readUntilKind(ctx, member, rendezvous.KindAck)
		if err != nil || ack.MessageID != queuedMessageID || ack.State != rendezvous.DeliveryQueued {
			return Report{}, errors.New("rendezvous contract: queued delivery acknowledgement mismatch")
		}
	}
	offlineIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		return Report{}, fmt.Errorf("rendezvous contract: create mailbox identity: %w", err)
	}
	offline, err := dialRegistered(ctx, endpoint.WebSocketURL, cfg.Origin, topic, offlinePresence, rendezvous.RoleMember, offlineIdentity)
	if err != nil {
		return Report{}, fmt.Errorf("rendezvous contract: register mailbox target: %w", err)
	}
	defer offline.CloseNow()
	queued, err := readUntilKind(ctx, offline, rendezvous.KindSignal)
	if err != nil || queued.MessageID != queuedMessageID || queued.From != memberPresence || queued.Payload != queuedPayload {
		return Report{}, errors.New("rendezvous contract: mailbox signal mismatch")
	}
	report.MailboxDelivery = true
	duplicateCtx, duplicateCancel := context.WithTimeout(ctx, 150*time.Millisecond)
	_, _, duplicateErr := offline.Read(duplicateCtx)
	duplicateCancel()
	if duplicateErr == nil {
		return Report{}, errors.New("rendezvous contract: publish retry produced a duplicate signal")
	}
	report.RetryDeduplicated = true

	metrics, err := getText(ctx, endpoint.MetricsURL)
	if err != nil {
		return Report{}, fmt.Errorf("rendezvous contract: metrics: %w", err)
	}
	for _, secret := range []string{topic, hostPresence, memberPresence, offlinePresence, liveMessageID, queuedMessageID, livePayload, queuedPayload} {
		if strings.Contains(metrics, secret) {
			return Report{}, errors.New("rendezvous contract: metrics exposed routing or payload material")
		}
	}
	for _, metric := range []string{
		"atterm_rendezvous_active_connections", "atterm_rendezvous_registered_peers",
		"atterm_rendezvous_forwarded_messages_total", "atterm_rendezvous_queued_messages_total",
	} {
		if !strings.Contains(metrics, metric) {
			return Report{}, fmt.Errorf("rendezvous contract: metrics missing %s", metric)
		}
	}
	report.MetricsPrivate = true
	return report, nil
}

func checkHealth(ctx context.Context, healthURL string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return 0, fmt.Errorf("rendezvous contract: create health request: %w", err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("rendezvous contract: health request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("rendezvous contract: health status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil {
		return 0, fmt.Errorf("rendezvous contract: read health: %w", err)
	}
	if len(body) > 4096 {
		return 0, errors.New("rendezvous contract: health response is too large")
	}
	var payload struct {
		Status          string `json:"status"`
		ProtocolVersion int    `json:"protocol_version"`
	}
	if err := decodeSingleJSON(body, &payload); err != nil {
		return 0, fmt.Errorf("rendezvous contract: decode health: %w", err)
	}
	if payload.Status != "ok" || payload.ProtocolVersion != rendezvous.Version {
		return 0, errors.New("rendezvous contract: incompatible health response")
	}
	return payload.ProtocolVersion, nil
}

func dialRegistered(ctx context.Context, endpoint, origin, topic, presenceID string, role rendezvous.Role, identity *peercrypto.Identity) (*websocket.Conn, error) {
	header := make(http.Header)
	if origin != "" {
		header.Set("Origin", origin)
	}
	conn, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		Subprotocols: []string{rendezvous.Subprotocol}, HTTPHeader: header,
	})
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("dial status %d: %w", response.StatusCode, err)
		}
		return nil, fmt.Errorf("dial: %w", err)
	}
	if conn.Subprotocol() != rendezvous.Subprotocol {
		conn.CloseNow()
		return nil, errors.New("server did not negotiate the Rendezvous subprotocol")
	}
	challenge, err := readUntilKind(ctx, conn, rendezvous.KindChallenge)
	if err != nil {
		conn.CloseNow()
		return nil, err
	}
	registration, err := rendezvous.SignRegistration(identity, challenge.Challenge, topic, presenceID, role)
	if err != nil {
		conn.CloseNow()
		return nil, err
	}
	if err := writeJSON(ctx, conn, registration); err != nil {
		conn.CloseNow()
		return nil, err
	}
	registered, err := readUntilKind(ctx, conn, rendezvous.KindRegistered)
	if err != nil || registered.Version != rendezvous.Version {
		conn.CloseNow()
		return nil, errors.New("registration was not acknowledged")
	}
	return conn, nil
}

func readUntilKind(ctx context.Context, conn *websocket.Conn, kind string) (rendezvous.EventMessage, error) {
	for attempts := 0; attempts < 16; attempts++ {
		messageType, payload, err := conn.Read(ctx)
		if err != nil {
			return rendezvous.EventMessage{}, err
		}
		if messageType != websocket.MessageText {
			return rendezvous.EventMessage{}, errors.New("rendezvous contract: received non-text event")
		}
		event, err := decodeEvent(payload)
		if err != nil {
			return rendezvous.EventMessage{}, errors.New("rendezvous contract: invalid event envelope")
		}
		if event.Kind == kind {
			return event, nil
		}
		if event.Kind == rendezvous.KindError {
			return rendezvous.EventMessage{}, fmt.Errorf("rendezvous contract: server returned error %q", event.Code)
		}
	}
	return rendezvous.EventMessage{}, fmt.Errorf("rendezvous contract: event %q was not received", kind)
}

func decodeEvent(payload []byte) (rendezvous.EventMessage, error) {
	var event rendezvous.EventMessage
	if err := decodeSingleJSON(payload, &event); err != nil || event.Version != rendezvous.Version {
		return rendezvous.EventMessage{}, errors.New("invalid event envelope")
	}
	return event, nil
}

func decodeSingleJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func writeJSON(ctx context.Context, conn *websocket.Conn, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("rendezvous contract: encode message: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		return fmt.Errorf("rendezvous contract: write message: %w", err)
	}
	return nil
}

func getText(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func randomOpaqueID(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("rendezvous contract: create opaque value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
