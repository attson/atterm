package serviceproxy

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestCodecRoundTripAndReplay(t *testing.T) {
	id := uuid.MustParse("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	c2h := bytes.Repeat([]byte{0x11}, 32)
	h2c := bytes.Repeat([]byte{0x22}, 32)
	client, err := NewCodec(id, c2h, h2c)
	if err != nil {
		t.Fatal(err)
	}
	host, err := NewCodec(id, h2c, c2h)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := client.Seal(KindData, 7, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	h, got, err := host.Open(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if h.Connection != 7 || h.Kind != KindData || string(got) != "hello" {
		t.Fatalf("opened = %+v %q", h, got)
	}
	if _, _, err := host.Open(pkt); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay error = %v", err)
	}

	reply, err := host.Seal(KindData, 7, []byte("world"))
	if err != nil {
		t.Fatal(err)
	}
	_, got, err = client.Open(reply)
	if err != nil || string(got) != "world" {
		t.Fatalf("reply = %q, %v", got, err)
	}
}

func TestCodecRejectsTamperAndCrossService(t *testing.T) {
	key := bytes.Repeat([]byte{0x33}, 32)
	id := uuid.New()
	a, _ := NewCodec(id, key, key)
	b, _ := NewCodec(id, key, key)
	pkt, _ := a.Seal(KindData, 1, []byte("secret"))
	tampered := append([]byte(nil), pkt...)
	tampered[len(tampered)-1] ^= 1
	if _, _, err := b.Open(tampered); err == nil {
		t.Fatal("tampered packet opened")
	}

	other, _ := NewCodec(uuid.New(), key, key)
	if _, _, err := other.Open(pkt); err == nil {
		t.Fatal("packet opened under another service id")
	}
}

func TestParseHeaderBounds(t *testing.T) {
	if _, err := ParseHeader(make([]byte, HeaderSize+GCMTagSize)); err == nil {
		t.Fatal("zero header accepted")
	}
	codec, _ := NewCodec(uuid.New(), bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{1}, 32))
	if _, err := codec.Seal(KindData, 1, make([]byte, MaxPlaintextSize+1)); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("oversize error = %v", err)
	}
}
