package webhook

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func sampleEvent() CommandFinished {
	return CommandFinished{
		SessionID: uuid.MustParse("00000000-0000-0000-0000-0000000000a1"),
		HostID:    uuid.MustParse("00000000-0000-0000-0000-0000000000b2"),
		ExitCode:  0,
		ElapsedMS: 2300,
		Label:     "npm test",
	}
}

func TestRenderGeneric(t *testing.T) {
	body := renderGeneric(sampleEvent())
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("generic body not valid json: %v", err)
	}
	if parsed["label"] != "npm test" || parsed["exit_code"].(float64) != 0 || parsed["elapsed_ms"].(float64) != 2300 {
		t.Fatalf("generic payload wrong: %+v", parsed)
	}
	if parsed["session_id"] == "" || parsed["host_id"] == "" {
		t.Fatalf("generic payload missing ids: %+v", parsed)
	}
}

// TestRenderGeneric_Sealed: generic JSON renderer must omit label/exit/
// elapsed when sealed and instead carry a base64 sealed_body the
// receiver can decrypt out-of-band.
func TestRenderGeneric_Sealed(t *testing.T) {
	ev := sampleEvent()
	ev.SealedBody = []byte{0x01, 0x02, 0x03, 0x04}
	body := renderGeneric(ev)
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("generic body not valid json: %v", err)
	}
	if _, ok := parsed["label"]; ok {
		t.Fatalf("generic sealed payload leaked label: %+v", parsed)
	}
	if _, ok := parsed["exit_code"]; ok {
		t.Fatalf("generic sealed payload leaked exit_code: %+v", parsed)
	}
	if _, ok := parsed["elapsed_ms"]; ok {
		t.Fatalf("generic sealed payload leaked elapsed_ms: %+v", parsed)
	}
	got, _ := parsed["sealed_body"].(string)
	if got != "AQIDBA==" { // base64.StdEncoding of {0x01,0x02,0x03,0x04}
		t.Fatalf("sealed_body = %q; want base64 of envelope", got)
	}
	if parsed["text"] != "Session command finished" {
		t.Fatalf("text = %v; want generic", parsed["text"])
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := map[int]string{500: "0.5s", 2300: "2.3s", 64000: "1m04s", 3600000: "60m00s"}
	for ms, want := range cases {
		if got := formatElapsed(ms); got != want {
			t.Errorf("formatElapsed(%d) = %q, want %q", ms, got, want)
		}
	}
}
