package main

import (
	"encoding/json"
	"strings"
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

func TestRetiredSessionCreateRelayResponse(t *testing.T) {
	for _, tc := range []struct {
		name      string
		payload   string
		requestID string
		errorCode string
	}{
		{name: "valid", payload: ` {"request_id":"valid","host_id":"host-1","profile_id":"p1"} `, requestID: "valid", errorCode: "feature_removed"},
		{name: "bounded_id", payload: `{"request_id":"` + strings.Repeat("x", 256) + `","host_id":"host-1","profile_id":"p1"}`, requestID: strings.Repeat("x", 256), errorCode: "feature_removed"},
		{name: "unknown_before_id", payload: `{"shell":"/bin/sh","request_id":"unknown","host_id":"host-1","profile_id":"p1"}`, requestID: "unknown", errorCode: "invalid_request"},
		{name: "wrong_type_before_id", payload: `{"host_id":42,"request_id":"typed","profile_id":"p1"}`, requestID: "typed", errorCode: "invalid_request"},
		{name: "missing_host", payload: `{"request_id":"no-host","profile_id":"p1"}`, requestID: "no-host", errorCode: "invalid_request"},
		{name: "missing_profile", payload: `{"request_id":"no-profile","host_id":"host-1"}`, requestID: "no-profile", errorCode: "invalid_request"},
		{name: "empty_host", payload: `{"request_id":"empty-host","host_id":"","profile_id":"p1"}`, requestID: "empty-host", errorCode: "invalid_request"},
		{name: "empty_profile", payload: `{"request_id":"empty-profile","host_id":"host-1","profile_id":""}`, requestID: "empty-profile", errorCode: "invalid_request"},
		{name: "nested_id_is_not_correlation", payload: `{"extra":{"request_id":"nested"},"host_id":"host-1","profile_id":"p1"}`},
		{name: "nested_unknown", payload: `{"request_id":"outer","extra":{"request_id":"nested"}}`, requestID: "outer", errorCode: "invalid_request"},
		{name: "empty_payload"},
		{name: "invalid_json", payload: `{`},
		{name: "truncated_after_id", payload: `{"request_id":"truncated","host_id":`},
		{name: "null", payload: `null`},
		{name: "array", payload: `[{"request_id":"array"}]`},
		{name: "scalar", payload: `"request_id"`},
		{name: "missing_id", payload: `{"host_id":"host-1","profile_id":"p1"}`},
		{name: "empty_id", payload: `{"request_id":""}`},
		{name: "oversized_id", payload: `{"request_id":"` + strings.Repeat("x", 257) + `"}`},
		{name: "non_string_id", payload: `{"request_id":123}`},
		{name: "null_id", payload: `{"request_id":null}`},
		{name: "duplicate_id", payload: `{"request_id":"first","request_id":"second"}`},
		{name: "duplicate_same_id", payload: `{"request_id":"same","request_id":"same"}`},
		{name: "escaped_duplicate_id", payload: `{"request_id":"first","\u0072equest_id":"second"}`},
		{name: "case_alias", payload: `{"request_id":"first","REQUEST_ID":"second"}`},
		{name: "alias_before_id", payload: `{"REQUEST_ID":"first","request_id":"second"}`},
		{name: "trailing_object", payload: `{"request_id":"first","host_id":"host-1","profile_id":"p1"} {}`},
		{name: "trailing_junk", payload: `{"request_id":"first"} junk`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame, ok := retiredRelaySessionCreateFrame([]byte(tc.payload))
			if tc.requestID == "" {
				if ok {
					t.Fatalf("unaddressable payload emitted response: %+v", frame)
				}
				return
			}
			if !ok || frame.Type != proto.TypeSessionCreated || frame.SessionID != uuid.Nil {
				t.Fatalf("addressable response: ok=%v frame=%+v", ok, frame)
			}
			var payload proto.SessionCreatedPayload
			if err := json.Unmarshal(frame.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.RequestID != tc.requestID || payload.OK || payload.SessionID != "" || payload.Error != tc.errorCode {
				t.Fatalf("response=%+v, want request_id=%q error=%q", payload, tc.requestID, tc.errorCode)
			}
		})
	}
}
