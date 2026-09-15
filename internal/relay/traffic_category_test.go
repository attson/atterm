package relay

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
)

func TestFrameCategory(t *testing.T) {
	cases := map[proto.Type]string{
		proto.TypeIn:             "terminal",
		proto.TypeOut:            "terminal",
		proto.TypeResize:         "terminal",
		proto.TypePasteImage:     "terminal",
		proto.TypePasteFile:      "terminal",
		proto.TypeMeta:           "state",
		proto.TypeList:           "state",
		proto.TypeListResp:       "state",
		proto.TypeReplayProgress: "state",
		proto.TypeAnnounce:       "state",
		proto.TypeViewers:        "state",
		proto.TypeOpen:           "state",
		proto.TypeClose:          "state",
		proto.TypeStreamRequest:  "state",
		proto.TypeStreamStop:     "state",
		proto.TypeCommandEvent:   "state",
		proto.TypeSessionCreate:  "state",
		proto.TypeSessionCreated: "state",
		proto.TypeAttach:         "state",
		proto.TypeClaimDriver:    "state",
		proto.TypePing:           "state",
		proto.TypePong:           "state",
		proto.TypePrefsChanged:   "config",
		proto.TypeAuthInfo:       "config",
		proto.TypeFSRequest:      "fs",
		proto.TypeFSResponse:     "fs",
		proto.TypeFSEvent:        "fs",
		proto.TypeServiceOpen:    "preview",
		proto.TypeServiceOpened:  "preview",
		proto.TypeServiceClose:   "preview",
	}
	for ft, want := range cases {
		if got := frameCategory(ft); got != want {
			t.Errorf("frameCategory(0x%02x) = %q, want %q", byte(ft), got, want)
		}
	}
	// Unknown type falls back to "other".
	if got := frameCategory(proto.Type(0xff)); got != "other" {
		t.Errorf("frameCategory(unknown) = %q, want other", got)
	}
}
