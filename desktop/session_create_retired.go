package main

import (
	"encoding/json"

	"github.com/attson/atterm/internal/proto"
)

const sessionCreateErrFeatureRemoved = "feature_removed"

func retiredSessionCreateFrame(requestID string) proto.Frame {
	payload, _ := json.Marshal(proto.SessionCreatedPayload{
		RequestID: requestID,
		Error:     sessionCreateErrFeatureRemoved,
	})
	return proto.Frame{Type: proto.TypeSessionCreated, Payload: payload}
}
