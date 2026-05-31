package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRecordRelayError_RingBufferKeeps5Newest(t *testing.T) {
	a := newRelayTestApp(t)
	for i := 0; i < 8; i++ {
		a.recordRelayError(fmt.Errorf("err-%d", i))
	}
	got := a.snapshotRelayErrors()
	if len(got) != 5 {
		t.Fatalf("want 5, got %d", len(got))
	}
	if got[0].Message != "err-7" {
		t.Fatalf("newest first failed: %q", got[0].Message)
	}
	if got[4].Message != "err-3" {
		t.Fatalf("oldest in buffer wrong: %q", got[4].Message)
	}
}

func TestRecordRelayError_NilIsNoop(t *testing.T) {
	a := newRelayTestApp(t)
	a.recordRelayError(nil)
	if got := a.snapshotRelayErrors(); len(got) != 0 {
		t.Fatalf("nil should not record, got %d entries", len(got))
	}
}

func TestRecordRelayError_RedactsTokensInMessage(t *testing.T) {
	a := newRelayTestApp(t)
	a.recordRelayError(errors.New("401 dial failed: atk_abcdefghij blocked"))
	got := a.snapshotRelayErrors()
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if !strings.Contains(got[0].Message, "atk_abcdefgh…") {
		t.Fatalf("expected redacted token, got %q", got[0].Message)
	}
}

func TestSnapshotRelayErrors_ReturnsCopy(t *testing.T) {
	a := newRelayTestApp(t)
	a.recordRelayError(fmt.Errorf("e"))
	snap := a.snapshotRelayErrors()
	snap[0].Message = "mutated"
	again := a.snapshotRelayErrors()
	if again[0].Message != "e" {
		t.Fatalf("internal state was mutated by caller: %q", again[0].Message)
	}
}
