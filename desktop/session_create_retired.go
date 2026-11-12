package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/attson/atterm/internal/proto"
)

const sessionCreateErrFeatureRemoved = "feature_removed"

func retiredSessionCreateFrame(requestID string) proto.Frame {
	return sessionCreateErrorFrame(requestID, sessionCreateErrFeatureRemoved)
}

func sessionCreateErrorFrame(requestID, errorCode string) proto.Frame {
	payload, _ := json.Marshal(proto.SessionCreatedPayload{
		RequestID: requestID,
		Error:     errorCode,
	})
	return proto.Frame{Type: proto.TypeSessionCreated, Payload: payload}
}

func retiredRelaySessionCreateFrame(payload []byte) (proto.Frame, bool) {
	requestID := recoverSessionCreateRequestID(payload)
	if requestID == "" {
		return proto.Frame{}, false
	}
	var request proto.SessionCreatePayload
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		request.HostID == "" || request.ProfileID == "" {
		return sessionCreateErrorFrame(requestID, "invalid_request"), true
	}
	return retiredSessionCreateFrame(requestID), true
}

func recoverSessionCreateRequestID(payload []byte) string {
	// A map or struct would silently choose the last duplicate id. Require one
	// complete object and reject aliases that encoding/json matches without case.
	if !json.Valid(payload) {
		return ""
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return ""
	}
	var requestID string
	seenID := false
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		key, ok := token.(string)
		if !ok {
			return ""
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return ""
		}
		if !strings.EqualFold(key, "request_id") {
			continue
		}
		if seenID || key != "request_id" {
			return ""
		}
		seenID = true
		if err := json.Unmarshal(value, &requestID); err != nil || requestID == "" || len(requestID) > 256 {
			return ""
		}
	}
	return requestID
}
