package proto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestMarshalUnmarshalRoundtrip(t *testing.T) {
	sid := uuid.MustParse("12345678-1234-1234-1234-1234567890ab")
	in := Frame{
		Type:      TypeOut,
		SessionID: sid,
		Payload:   []byte("hello world"),
	}
	got, err := Unmarshal(Marshal(in))
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if got.Type != in.Type || got.SessionID != in.SessionID || !bytes.Equal(got.Payload, in.Payload) {
		t.Fatalf("roundtrip mismatch: got=%+v want=%+v", got, in)
	}
}

func TestMarshalEmptyPayloadRoundtrips(t *testing.T) {
	in := Frame{Type: TypePong, SessionID: uuid.Nil}
	out, err := Unmarshal(Marshal(in))
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if out.Type != TypePong || len(out.Payload) != 0 {
		t.Fatalf("got %+v; want empty pong frame", out)
	}
}

func TestMarshalEncodesHeaderInBigEndian(t *testing.T) {
	payload := bytes.Repeat([]byte{0xAA}, 300)
	frame := Marshal(Frame{Type: TypeOut, SessionID: uuid.Nil, Payload: payload})
	if frame[0] != Version {
		t.Fatalf("version byte = %d; want %d", frame[0], Version)
	}
	if Type(frame[1]) != TypeOut {
		t.Fatalf("type byte = %#x; want %#x", frame[1], TypeOut)
	}
	plen := binary.BigEndian.Uint32(frame[2:6])
	if plen != uint32(len(payload)) {
		t.Fatalf("encoded payload len = %d; want %d", plen, len(payload))
	}
}

func TestUnmarshalRejectsShortFrame(t *testing.T) {
	_, err := Unmarshal([]byte{1, byte(TypePong)})
	if !errors.Is(err, ErrShort) {
		t.Fatalf("err = %v; want ErrShort", err)
	}
}

func TestUnmarshalRejectsBadVersion(t *testing.T) {
	in := Marshal(Frame{Type: TypePong, SessionID: uuid.Nil})
	in[0] = 99
	_, err := Unmarshal(in)
	if !errors.Is(err, ErrVersion) {
		t.Fatalf("err = %v; want ErrVersion", err)
	}
	if !strings.Contains(err.Error(), "99") {
		t.Fatalf("err = %q; want wrapped detail mentioning version 99", err)
	}
}

func TestUnmarshalRejectsOversizedPayloadLength(t *testing.T) {
	// Synthesize a header that claims a payload larger than maxPayload, with
	// no actual payload bytes — the length check fires before the mismatch
	// check, so we get ErrTooLarge.
	in := make([]byte, minFrameLen)
	in[0] = Version
	in[1] = byte(TypeOut)
	binary.BigEndian.PutUint32(in[2:6], maxPayload+1)
	_, err := Unmarshal(in)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v; want ErrTooLarge", err)
	}
}

func TestUnmarshalRejectsLengthMismatch(t *testing.T) {
	in := Marshal(Frame{Type: TypeOut, SessionID: uuid.Nil, Payload: []byte("abc")})
	in = in[:len(in)-1] // truncate one payload byte
	_, err := Unmarshal(in)
	if !errors.Is(err, ErrPayloadLen) {
		t.Fatalf("err = %v; want ErrPayloadLen", err)
	}
}

func TestUnmarshalRejectsTrailingGarbage(t *testing.T) {
	in := Marshal(Frame{Type: TypeOut, SessionID: uuid.Nil, Payload: []byte("abc")})
	in = append(in, 0xFF) // an extra byte past the declared payload length
	_, err := Unmarshal(in)
	if !errors.Is(err, ErrPayloadLen) {
		t.Fatalf("err = %v; want ErrPayloadLen", err)
	}
}

func TestUnmarshalReturnsDefensiveCopy(t *testing.T) {
	// Mutating the wire bytes after Unmarshal must not bleed into the decoded
	// payload — important since callers may reuse the read buffer.
	wire := Marshal(Frame{Type: TypeOut, SessionID: uuid.Nil, Payload: []byte("abc")})
	got, err := Unmarshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	wire[minFrameLen] = 'Z'
	if string(got.Payload) != "abc" {
		t.Fatalf("payload = %q; want abc (caller-side wire mutation leaked through)", got.Payload)
	}
}

func TestEncodeDecodeOutRoundtrip(t *testing.T) {
	sid := uuid.New()
	frame := EncodeOut(sid, 42, []byte("payload"))
	if frame.Type != TypeOut {
		t.Fatalf("Type = %#x; want TypeOut", frame.Type)
	}
	if frame.SessionID != sid {
		t.Fatalf("SessionID = %v; want %v", frame.SessionID, sid)
	}
	seq, data, err := DecodeOut(frame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 42 || string(data) != "payload" {
		t.Fatalf("seq=%d data=%q; want 42/payload", seq, data)
	}
}

func TestDecodeOutRejectsShortPayload(t *testing.T) {
	_, _, err := DecodeOut([]byte{1, 2, 3})
	if !errors.Is(err, ErrShort) {
		t.Fatalf("err = %v; want ErrShort", err)
	}
}

func TestDecodeOutAcceptsEmptyDataAfterSeq(t *testing.T) {
	frame := EncodeOut(uuid.Nil, 7, nil)
	seq, data, err := DecodeOut(frame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 7 || len(data) != 0 {
		t.Fatalf("seq=%d len(data)=%d; want 7/0", seq, len(data))
	}
}

func TestEncodeDecodeResizeRoundtrip(t *testing.T) {
	for _, c := range []struct{ cols, rows uint16 }{
		{80, 24},
		{0, 0},
		{1, 1},
		{0xFFFF, 0xFFFF},
		{132, 43},
	} {
		got1, got2, err := DecodeResize(EncodeResize(c.cols, c.rows))
		if err != nil {
			t.Fatalf("(%d,%d): %v", c.cols, c.rows, err)
		}
		if got1 != c.cols || got2 != c.rows {
			t.Fatalf("got %dx%d; want %dx%d", got1, got2, c.cols, c.rows)
		}
	}
}

func TestDecodeResizeRejectsShortPayload(t *testing.T) {
	_, _, err := DecodeResize([]byte{1, 2, 3})
	if !errors.Is(err, ErrShort) {
		t.Fatalf("err = %v; want ErrShort", err)
	}
}

func TestMarshalLargeButValidPayloadRoundtrips(t *testing.T) {
	// Exercises the boundary where payload is large but within maxPayload —
	// catches accidental int32/int conversion bugs in the length header.
	body := bytes.Repeat([]byte{'A'}, 1<<20) // 1 MiB
	in := Frame{Type: TypeOut, SessionID: uuid.New(), Payload: body}
	out, err := Unmarshal(Marshal(in))
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if !bytes.Equal(out.Payload, body) {
		t.Fatalf("payload mismatch (len=%d)", len(out.Payload))
	}
}
