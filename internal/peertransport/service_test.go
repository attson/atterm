package peertransport

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestServiceMessageRoundTripAndBounds(t *testing.T) {
	id := uuid.New()
	data := bytes.Repeat([]byte{0x5a}, MaxServiceData)
	encoded, err := EncodeServiceMessage(ServiceMessage{
		ServiceID: id, Kind: ServiceData, Connection: 7, Data: data,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != MaxRecordPlaintext {
		t.Fatalf("encoded length = %d, want %d", len(encoded), MaxRecordPlaintext)
	}
	decoded, err := DecodeServiceMessage(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ServiceID != id || decoded.Kind != ServiceData || decoded.Connection != 7 || !bytes.Equal(decoded.Data, data) {
		t.Fatalf("decoded = %+v", decoded)
	}

	for _, message := range []ServiceMessage{
		{ServiceID: uuid.Nil, Kind: ServiceOpen, Connection: 1},
		{ServiceID: id, Kind: 0, Connection: 1},
		{ServiceID: id, Kind: ServiceOpen, Connection: 0},
		{ServiceID: id, Kind: ServiceOpen, Connection: 1, Data: []byte("unexpected")},
		{ServiceID: id, Kind: ServiceData, Connection: 1},
		{ServiceID: id, Kind: ServiceData, Connection: 1, Data: make([]byte, MaxServiceData+1)},
	} {
		if _, err := EncodeServiceMessage(message); !errors.Is(err, ErrInvalidServiceMessage) {
			t.Fatalf("EncodeServiceMessage(%+v) error = %v", message, err)
		}
	}
}

func TestDecodeServiceMessageRejectsMalformedPayload(t *testing.T) {
	id := uuid.New()
	valid, err := EncodeServiceMessage(ServiceMessage{ServiceID: id, Kind: ServiceClose, Connection: 9})
	if err != nil {
		t.Fatal(err)
	}
	malformed := [][]byte{
		nil,
		valid[:len(valid)-1],
		append(append([]byte(nil), valid...), 0),
		append([]byte(nil), valid...),
	}
	malformed[3][16] = 99
	for _, payload := range malformed {
		if _, err := DecodeServiceMessage(payload); !errors.Is(err, ErrInvalidServiceMessage) {
			t.Fatalf("DecodeServiceMessage(%d bytes) error = %v", len(payload), err)
		}
	}
}
