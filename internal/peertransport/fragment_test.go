package peertransport

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestFragmentReassemblyTenMiB(t *testing.T) {
	frame := make([]byte, 10*1024*1024)
	for i := range frame {
		frame[i] = byte(i % 251)
	}
	fragments, err := FragmentFrame(77, frame)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) < 2 {
		t.Fatal("large frame was not fragmented")
	}
	now := time.Unix(1_800_000_000, 0)
	var reassembler Reassembler
	var got []byte
	for i, fragment := range fragments {
		if len(fragment) > MaxRecordPlaintext {
			t.Fatalf("fragment %d is %d bytes", i, len(fragment))
		}
		var complete bool
		got, complete, err = reassembler.Add(fragment, now)
		if err != nil {
			t.Fatalf("fragment %d: %v", i, err)
		}
		if complete != (i == len(fragments)-1) {
			t.Fatalf("fragment %d complete=%v", i, complete)
		}
	}
	if !bytes.Equal(got, frame) {
		t.Fatal("reassembled frame differs")
	}
}

func TestReassemblerRejectsGapInterleaveAndTimeout(t *testing.T) {
	frame := bytes.Repeat([]byte{0x55}, MaxRecordPlaintext*2)
	fragments, _ := FragmentFrame(1, frame)
	now := time.Unix(1_800_000_000, 0)

	var gap Reassembler
	if _, _, err := gap.Add(fragments[1], now); !errors.Is(err, ErrInvalidFragment) {
		t.Fatalf("gap accepted: %v", err)
	}

	var interleaved Reassembler
	if _, _, err := interleaved.Add(fragments[0], now); err != nil {
		t.Fatal(err)
	}
	other := append([]byte(nil), fragments[1]...)
	other[7] = 2
	if _, _, err := interleaved.Add(other, now); !errors.Is(err, ErrInvalidFragment) {
		t.Fatalf("interleaved message accepted: %v", err)
	}

	var expired Reassembler
	if _, _, err := expired.Add(fragments[0], now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := expired.Add(fragments[1], now.Add(ReassemblyTimeout+time.Millisecond)); !errors.Is(err, ErrReassemblyTimeout) {
		t.Fatalf("expired message accepted: %v", err)
	}
}

func TestFragmentBounds(t *testing.T) {
	if _, err := FragmentFrame(1, make([]byte, MaxRecordPlaintext)); !errors.Is(err, ErrInvalidFragment) {
		t.Fatalf("small frame accepted: %v", err)
	}
	if _, err := FragmentFrame(1, make([]byte, MaxFrameSize+1)); !errors.Is(err, ErrInvalidFragment) {
		t.Fatalf("oversize frame accepted: %v", err)
	}
}
