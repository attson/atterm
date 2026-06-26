// internal/feishu/event.go
package feishu

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrNotEncryptedBody is returned by DecryptEnvelope when the request
// body lacks the top-level `encrypt` string. The relay rejects such
// requests because our spec mandates the user enable event encryption.
var ErrNotEncryptedBody = errors.New("feishu: body is not encrypted (no `encrypt` field)")

// ErrTokenMismatch indicates the envelope's verify token did not match
// the binding's stored verify_token.
var ErrTokenMismatch = errors.New("feishu: envelope verify-token mismatch")

// outerEncrypted is the shape Feishu sends for encrypted events.
type outerEncrypted struct {
	Encrypt string `json:"encrypt"`
}

// DecryptEnvelope decrypts the outer body using Feishu's documented
// scheme: AES-256-CBC with key=SHA256(encryptKey), IV=ciphertext[:16],
// data=ciphertext[16:], PKCS7-padded.
func DecryptEnvelope(body []byte, encryptKey string) ([]byte, error) {
	var outer outerEncrypted
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, fmt.Errorf("feishu: parse outer body: %w", err)
	}
	if outer.Encrypt == "" {
		return nil, ErrNotEncryptedBody
	}
	raw, err := base64.StdEncoding.DecodeString(outer.Encrypt)
	if err != nil {
		return nil, fmt.Errorf("feishu: base64 decode: %w", err)
	}
	if len(raw) < aes.BlockSize+aes.BlockSize {
		return nil, fmt.Errorf("feishu: ciphertext too short")
	}
	keyHash := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return nil, fmt.Errorf("feishu: aes cipher: %w", err)
	}
	iv, ct := raw[:aes.BlockSize], raw[aes.BlockSize:]
	if len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("feishu: ciphertext not block-aligned")
	}
	plain := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ct)
	// PKCS7 unpad
	if len(plain) == 0 {
		return nil, fmt.Errorf("feishu: empty plaintext")
	}
	padLen := int(plain[len(plain)-1])
	if padLen == 0 || padLen > aes.BlockSize || padLen > len(plain) {
		return nil, fmt.Errorf("feishu: invalid padding")
	}
	for i := len(plain) - padLen; i < len(plain); i++ {
		if int(plain[i]) != padLen {
			return nil, fmt.Errorf("feishu: invalid padding bytes")
		}
	}
	return plain[:len(plain)-padLen], nil
}

// Envelope is the parsed plaintext we operate on after decryption.
// At most one of (URLVerification, Header+Event-class fields) is set.
type Envelope struct {
	URLVerification *URLVerification
	Header          *EnvelopeHeader
	Message         *MessageReceive
	CardAction      *CardActionTrigger
}

type URLVerification struct {
	Challenge string `json:"challenge"`
	Token     string `json:"token"`
}

type EnvelopeHeader struct {
	EventType string `json:"event_type"`
	Token     string `json:"token"`
	AppID     string `json:"app_id"`
}

type MessageReceive struct {
	SenderOpenID string
	Text         string
}

type CardActionTrigger struct {
	OperatorOpenID string
	Kind           string
	SessionID      string
	Event          string
	Text           string
}

// ParseEnvelope inspects plaintext (already decrypted) and routes it to
// one of the typed sub-payloads. Unknown event_types parse the header
// only; callers can ignore them.
func ParseEnvelope(plaintext []byte) (*Envelope, error) {
	var probe struct {
		Type      string          `json:"type"`
		Challenge string          `json:"challenge"`
		Token     string          `json:"token"`
		Header    *EnvelopeHeader `json:"header"`
		Event     json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(plaintext, &probe); err != nil {
		return nil, fmt.Errorf("feishu: parse envelope: %w", err)
	}
	env := &Envelope{}
	if probe.Type == "url_verification" {
		env.URLVerification = &URLVerification{Challenge: probe.Challenge, Token: probe.Token}
		return env, nil
	}
	env.Header = probe.Header
	if probe.Header == nil {
		return env, nil
	}
	switch probe.Header.EventType {
	case "im.message.receive_v1":
		var ev struct {
			Sender struct {
				SenderID struct {
					OpenID string `json:"open_id"`
				} `json:"sender_id"`
			} `json:"sender"`
			Message struct {
				Content     string `json:"content"`
				MessageType string `json:"message_type"`
			} `json:"message"`
		}
		if err := json.Unmarshal(probe.Event, &ev); err != nil {
			return nil, fmt.Errorf("feishu: parse im.message: %w", err)
		}
		// content is itself JSON string like {"text":"..."}
		var inner struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal([]byte(ev.Message.Content), &inner)
		env.Message = &MessageReceive{
			SenderOpenID: ev.Sender.SenderID.OpenID,
			Text:         inner.Text,
		}
	case "card.action.trigger":
		var ev struct {
			Action struct {
				Value struct {
					Kind      string `json:"kind"`
					SessionID string `json:"session_id"`
					Event     string `json:"event"`
					Text      string `json:"text"`
				} `json:"value"`
			} `json:"action"`
			Operator struct {
				OpenID string `json:"open_id"`
			} `json:"operator"`
		}
		if err := json.Unmarshal(probe.Event, &ev); err != nil {
			return nil, fmt.Errorf("feishu: parse card.action: %w", err)
		}
		env.CardAction = &CardActionTrigger{
			OperatorOpenID: ev.Operator.OpenID,
			Kind:           ev.Action.Value.Kind,
			SessionID:      ev.Action.Value.SessionID,
			Event:          ev.Action.Value.Event,
			Text:           ev.Action.Value.Text,
		}
	}
	return env, nil
}

// VerifyEnvelopeToken asserts that the envelope's token (either from
// header for events or from url_verification) matches the binding's
// verify_token. Constant-time compare to avoid timing oracles.
func VerifyEnvelopeToken(env *Envelope, verifyToken string) error {
	var have string
	switch {
	case env.URLVerification != nil:
		have = env.URLVerification.Token
	case env.Header != nil:
		have = env.Header.Token
	default:
		return ErrTokenMismatch
	}
	if !constantTimeEq(have, verifyToken) {
		return ErrTokenMismatch
	}
	return nil
}

func constantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
