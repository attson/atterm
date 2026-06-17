// internal/feishu/event_test.go
package feishu

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// feishuTestEncrypt mirrors the Feishu encryption scheme so tests can
// craft ciphertext to feed DecryptEnvelope.
//
//	key = SHA256(encryptKey)
//	iv  = random 16 bytes
//	ct  = iv || AES-256-CBC(PKCS7(plain))
//	wrap{ "encrypt": base64(ct) }
func feishuTestEncrypt(t *testing.T, encryptKey string, plain []byte) []byte {
	t.Helper()
	key := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	iv := bytes.Repeat([]byte{0x42}, aes.BlockSize) // deterministic for test
	padLen := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append([]byte{}, plain...)
	for i := 0; i < padLen; i++ {
		padded = append(padded, byte(padLen))
	}
	ct := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ct, padded)
	combined := append(append([]byte{}, iv...), ct...)
	body, _ := json.Marshal(map[string]string{"encrypt": base64.StdEncoding.EncodeToString(combined)})
	return body
}

func TestDecryptEnvelope_HappyPath(t *testing.T) {
	plain := []byte(`{"header":{"event_type":"im.message.receive_v1","token":"vtok"},"event":{}}`)
	body := feishuTestEncrypt(t, "my-encrypt-key", plain)
	got, err := DecryptEnvelope(body, "my-encrypt-key")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("decrypted mismatch: %s vs %s", string(got), string(plain))
	}
}

func TestDecryptEnvelope_WrongKey(t *testing.T) {
	body := feishuTestEncrypt(t, "key-A", []byte(`{"x":1}`))
	if _, err := DecryptEnvelope(body, "key-B"); err == nil {
		t.Fatalf("expected decrypt failure with wrong key")
	}
}

func TestDecryptEnvelope_NoEncryptField(t *testing.T) {
	if _, err := DecryptEnvelope([]byte(`{"hello":"world"}`), "k"); !errors.Is(err, ErrNotEncryptedBody) {
		t.Fatalf("want ErrNotEncryptedBody, got %v", err)
	}
}

func TestParseEnvelope_URLVerification(t *testing.T) {
	plain := []byte(`{"type":"url_verification","challenge":"abc123","token":"vtok"}`)
	env, err := ParseEnvelope(plain)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if env.URLVerification == nil {
		t.Fatalf("expected url_verification")
	}
	if env.URLVerification.Challenge != "abc123" || env.URLVerification.Token != "vtok" {
		t.Fatalf("fields: %+v", env.URLVerification)
	}
}

func TestParseEnvelope_MessageReceive(t *testing.T) {
	plain := []byte(`{
	  "header":{
	    "event_type":"im.message.receive_v1",
	    "token":"vtok",
	    "app_id":"cli_abc"
	  },
	  "event":{
	    "sender":{"sender_id":{"open_id":"ou_xyz"}},
	    "message":{"content":"{\"text\":\"/bind AB3CD7\"}","message_type":"text"}
	  }
	}`)
	env, err := ParseEnvelope(plain)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if env.Header == nil || env.Header.EventType != "im.message.receive_v1" {
		t.Fatalf("header: %+v", env.Header)
	}
	if env.Message == nil || env.Message.SenderOpenID != "ou_xyz" {
		t.Fatalf("message: %+v", env.Message)
	}
	if env.Message.Text != "/bind AB3CD7" {
		t.Fatalf("text: %q", env.Message.Text)
	}
}

func TestParseEnvelope_CardAction(t *testing.T) {
	plain := []byte(`{
	  "header":{"event_type":"card.action.trigger","token":"vtok","app_id":"cli_abc"},
	  "event":{
	    "action":{"value":{"kind":"ack","session_id":"sid-1","event":"command_finished"}},
	    "operator":{"open_id":"ou_xyz"}
	  }
	}`)
	env, err := ParseEnvelope(plain)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if env.CardAction == nil {
		t.Fatalf("expected card action")
	}
	if env.CardAction.Kind != "ack" || env.CardAction.SessionID != "sid-1" || env.CardAction.Event != "command_finished" {
		t.Fatalf("action fields: %+v", env.CardAction)
	}
}

func TestVerifyEnvelopeToken(t *testing.T) {
	env := &Envelope{Header: &EnvelopeHeader{Token: "vtok"}}
	if err := VerifyEnvelopeToken(env, "vtok"); err != nil {
		t.Fatalf("match should pass: %v", err)
	}
	if err := VerifyEnvelopeToken(env, "other"); !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("mismatch should ErrTokenMismatch, got %v", err)
	}
	if err := VerifyEnvelopeToken(&Envelope{URLVerification: &URLVerification{Token: "vtok"}}, "vtok"); err != nil {
		t.Fatalf("url_verification token should pass: %v", err)
	}
}

func TestParseEnvelope_BadJSON(t *testing.T) {
	if _, err := ParseEnvelope([]byte("not-json")); err == nil || !strings.Contains(err.Error(), "parse envelope") {
		t.Fatalf("want parse error, got %v", err)
	}
}
