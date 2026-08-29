package e2eecrypto

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
)

func TestDeriveServiceKeysDomainSeparation(t *testing.T) {
	accountKey := bytes.Repeat([]byte{0x42}, SessionKeySize)
	id := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	got, err := DeriveServiceKeys(accountKey, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ClientToHost) != 32 || len(got.HostToClient) != 32 {
		t.Fatalf("key lengths = %d/%d", len(got.ClientToHost), len(got.HostToClient))
	}
	if bytes.Equal(got.ClientToHost, got.HostToClient) {
		t.Fatal("direction keys must differ")
	}
	if gotHex := hex.EncodeToString(got.ClientToHost); gotHex != "4d1906ee9238a25afc1142217b979f22b362b8d25b10377e4b280fa289950e8b" {
		t.Fatalf("client key vector = %s", gotHex)
	}
	if gotHex := hex.EncodeToString(got.HostToClient); gotHex != "d9647ac75246678dd15c7a614b13d82a70f479c9bde63d21a0dc59586313d95f" {
		t.Fatalf("host key vector = %s", gotHex)
	}
	other, err := DeriveServiceKeys(accountKey, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got.ClientToHost, other.ClientToHost) {
		t.Fatal("different service ids must derive different keys")
	}
}
