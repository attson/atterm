package main

import (
	"encoding/json"
	"testing"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestSealSessionInputHistoryResponseBindsSessionTypeAndRequest(t *testing.T) {
	accountKey := mustAccountKey32(t)
	sessionID := uuid.New()
	snapshot := SessionInputHistoryResult{
		Items:     []SessionInputHistoryItem{{Text: "secret prompt", SubmittedAtUnixMS: 1234}},
		Truncated: true,
	}

	response, err := sealSessionInputHistoryResponse(sessionID, "request-1", snapshot, accountKey)
	if err != nil {
		t.Fatalf("seal response: %v", err)
	}
	if !response.OK || response.RequestID != "request-1" || len(response.Sealed) == 0 {
		t.Fatalf("response head = %+v", response)
	}

	sessionKey, _ := e2eecrypto.DeriveSessionKey(accountKey, sessionID)
	plaintext, err := e2eecrypto.OpenUnsequenced(
		sessionKey, sessionID, byte(proto.TypeInputHistoryResponse), response.Sealed,
	)
	if err != nil {
		t.Fatalf("open response: %v", err)
	}
	var body proto.SealedInputHistoryResponse
	if err := json.Unmarshal(plaintext, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.RequestID != response.RequestID || len(body.Items) != 1 || body.Items[0].Text != "secret prompt" || !body.Truncated {
		t.Fatalf("opened body = %+v", body)
	}
	if _, err := e2eecrypto.OpenUnsequenced(sessionKey, uuid.New(), byte(proto.TypeInputHistoryResponse), response.Sealed); err == nil {
		t.Fatal("response opened under another session")
	}
	if _, err := e2eecrypto.OpenUnsequenced(sessionKey, sessionID, byte(proto.TypeMeta), response.Sealed); err == nil {
		t.Fatal("response opened under another frame type")
	}
}

func TestSealSessionInputHistoryResponseFailsClosedWithoutKey(t *testing.T) {
	response, err := sealSessionInputHistoryResponse(uuid.New(), "request-1", SessionInputHistoryResult{
		Items: []SessionInputHistoryItem{{Text: "must not leak"}},
	}, make([]byte, 16))
	if err == nil {
		t.Fatal("short key should fail")
	}
	if response.OK || len(response.Sealed) != 0 {
		t.Fatalf("failed response leaked success data: %+v", response)
	}
}
