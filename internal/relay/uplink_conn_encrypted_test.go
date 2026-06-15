package relay

import "testing"

func TestLooksLikeEncryptedOut_AcceptsEnvelope(t *testing.T) {
	// 1 cipher_id + 24 nonce + 16 tag = 41 bytes minimum
	env := make([]byte, 41)
	env[0] = 0x01
	if !looksLikeEncryptedOut(env) {
		t.Fatalf("expected true for minimum-size envelope")
	}
}

func TestLooksLikeEncryptedOut_RejectsShort(t *testing.T) {
	short := []byte{0x01, 0x02, 0x03}
	if looksLikeEncryptedOut(short) {
		t.Fatalf("expected false for 3-byte buffer")
	}
}

func TestLooksLikeEncryptedOut_RejectsWrongPrefix(t *testing.T) {
	plain := make([]byte, 100)
	plain[0] = 'h' // looks like the start of "hello\n"
	if looksLikeEncryptedOut(plain) {
		t.Fatalf("expected false for plaintext-looking buffer")
	}
}

func TestLooksLikeEncryptedOut_AcceptsRealisticChunk(t *testing.T) {
	// Realistic shape: cipher_id + 24B nonce + 30 bytes ciphertext + 16B tag.
	buf := make([]byte, 1+24+30+16)
	buf[0] = 0x01
	if !looksLikeEncryptedOut(buf) {
		t.Fatalf("expected true for realistic envelope size")
	}
}
