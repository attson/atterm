package main

import (
	"encoding/json"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestRetiredSessionCreateFrame(t *testing.T) {
	frame := retiredSessionCreateFrame("request-1")
	if frame.Type != proto.TypeSessionCreated || frame.SessionID != uuid.Nil {
		t.Fatalf("frame=%+v", frame)
	}
	var payload proto.SessionCreatedPayload
	if err := json.Unmarshal(frame.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RequestID != "request-1" || payload.OK || payload.SessionID != "" || payload.Error != "feature_removed" {
		t.Fatalf("payload=%+v", payload)
	}
}
