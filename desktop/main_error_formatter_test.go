package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"
)

// The frontend detects an unknown host key *structurally* — it reads
// Fingerprint/Host/HopIndex/HopName off the rejected value (see
// frontend/src/lib/sshHostKey.ts). Wails only ever sends Error() unless an
// ErrorFormatter is installed, so without frontendErrorFormatter the TOFU
// dialog can never open and the user sees the bare "ssh_host_key_unknown"
// sentinel with no way forward.
//
// The other half of the contract matters just as much: ErrorFormatter is
// cross-cutting, so *every* other rejected bound method must keep sending the
// exact string it sends today. These tests pin both halves.

func TestFrontendErrorFormatterHostKeyMarshalsToObject(t *testing.T) {
	hk := &HostKeyUnknownError{
		Fingerprint: "SHA256:abc",
		Host:        "[bastion]:2222",
		HopIndex:    2,
		HopName:     "prod-jump",
	}

	got := frontendErrorFormatter(hk)

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal formatted error: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("formatted error is not a JSON object: %v (%s)", err, raw)
	}

	if obj["Fingerprint"] != "SHA256:abc" {
		t.Errorf("Fingerprint = %#v, want %q", obj["Fingerprint"], "SHA256:abc")
	}
	if obj["Host"] != "[bastion]:2222" {
		t.Errorf("Host = %#v, want %q", obj["Host"], "[bastion]:2222")
	}
	// JSON numbers decode as float64.
	if obj["HopIndex"] != float64(2) {
		t.Errorf("HopIndex = %#v, want 2", obj["HopIndex"])
	}
	if obj["HopName"] != "prod-jump" {
		t.Errorf("HopName = %#v, want %q", obj["HopName"], "prod-jump")
	}
}

// The frontend reads capitalised, untagged field names. If anyone adds json
// tags to HostKeyUnknownError, or renames a field, sshHostKey.ts goes silently
// blind — parseHostKeyPrompt returns null and the dialog stops opening. Pin the
// wire keys exactly rather than trusting the struct definition.
func TestFrontendErrorFormatterHostKeyWireKeys(t *testing.T) {
	raw, err := json.Marshal(frontendErrorFormatter(&HostKeyUnknownError{
		Fingerprint: "SHA256:abc",
		Host:        "h",
	}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("not an object: %v (%s)", err, raw)
	}

	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	want := []string{"Fingerprint", "HopIndex", "HopName", "Host"}
	if len(keys) != len(want) {
		t.Fatalf("wire keys = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("wire keys = %v, want %v", keys, want)
		}
	}
}

// The error does not always arrive bare: dialThroughJumps and the connect path
// hand it up through layers that may annotate it. errors.As has to do the work,
// not a type assertion.
func TestFrontendErrorFormatterHostKeyWrapped(t *testing.T) {
	hk := &HostKeyUnknownError{Fingerprint: "SHA256:xyz", Host: "h", HopIndex: 1, HopName: "jump"}
	wrapped := fmt.Errorf("dial hop 1: %w", hk)
	doubly := fmt.Errorf("connect: %w", wrapped)

	for name, err := range map[string]error{"once": wrapped, "twice": doubly} {
		t.Run(name, func(t *testing.T) {
			got := frontendErrorFormatter(err)
			if _, ok := got.(string); ok {
				t.Fatalf("wrapped host-key error was flattened to a string: %#v", got)
			}
			raw, mErr := json.Marshal(got)
			if mErr != nil {
				t.Fatalf("marshal: %v", mErr)
			}
			var obj map[string]any
			if uErr := json.Unmarshal(raw, &obj); uErr != nil {
				t.Fatalf("not an object: %v (%s)", uErr, raw)
			}
			if obj["Fingerprint"] != "SHA256:xyz" || obj["Host"] != "h" {
				t.Fatalf("wrapped error lost its fields: %s", raw)
			}
			if obj["HopIndex"] != float64(1) || obj["HopName"] != "jump" {
				t.Fatalf("wrapped error lost its hop: %s", raw)
			}
		})
	}
}

// ErrorFormatter is installed once and applies to every bound method. Frontend
// callers overwhelmingly do `e instanceof Error ? e.message : String(e)`, which
// only reads right because a rejection is a string today. Anything that is not
// a host-key prompt must come back byte-for-byte as it does now.
func TestFrontendErrorFormatterEveryOtherErrorStaysAString(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"plain", errors.New("boom"), "boom"},
		{"wrapped", fmt.Errorf("outer: %w", errors.New("boom")), "outer: boom"},
		{"sentinel", errors.New(errCredentialMissing), errCredentialMissing},
		{"formatted", fmt.Errorf("connect %s: %d", "host", 22), "connect host: 22"},
		// The tunnel path deliberately does NOT wrap with %w — it renders the
		// prompt into human prose because a tunnel cannot answer TOFU. It must
		// keep arriving as that prose, not as an object the dialog would try to
		// open.
		{
			"tunnel host-key prose",
			tunnelHostKeyError(&HostKeyUnknownError{Fingerprint: "SHA256:abc", Host: "h"}),
			tunnelHostKeyError(&HostKeyUnknownError{Fingerprint: "SHA256:abc", Host: "h"}).Error(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := frontendErrorFormatter(tc.err)
			s, ok := got.(string)
			if !ok {
				t.Fatalf("got %#v (%T), want a string", got, got)
			}
			if s != tc.want {
				t.Fatalf("got %q, want %q", s, tc.want)
			}
			// And it must survive the JSON round trip as a bare string, since
			// that is what CallbackMessage.Err carries to the frontend.
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back string
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("did not round-trip as a JSON string: %v (%s)", err, raw)
			}
			if back != tc.want {
				t.Fatalf("round trip gave %q, want %q", back, tc.want)
			}
		})
	}
}
