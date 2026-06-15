package session

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// TestUpdateAdvertisedInfo_AdoptsSummary covers the gap between agent
// and relay that M2c exposed: now that the relay's mirror does not
// generate Summary from PushOut, the agent's ANNOUNCE-supplied Summary
// is the only source. Verify it actually flows through.
func TestUpdateAdvertisedInfo_AdoptsSummary(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	summary := &proto.SessionSummary{
		RecentOutput: "$ echo hi\nhi\n$ ",
		ErrorLines:   []string{"line1", "line2"},
		CapturedAt:   1234567890,
	}
	s.UpdateAdvertisedInfo(proto.SessionInfo{
		ID:      s.ID.String(),
		Summary: summary,
	})

	got := s.Info()
	if got.Summary == nil {
		t.Fatalf("Summary not adopted; got nil")
	}
	if got.Summary.RecentOutput != summary.RecentOutput {
		t.Fatalf("RecentOutput = %q, want %q", got.Summary.RecentOutput, summary.RecentOutput)
	}
	if got.Summary.CapturedAt != summary.CapturedAt {
		t.Fatalf("CapturedAt = %d, want %d", got.Summary.CapturedAt, summary.CapturedAt)
	}
	if len(got.Summary.ErrorLines) != 2 ||
		got.Summary.ErrorLines[0] != "line1" ||
		got.Summary.ErrorLines[1] != "line2" {
		t.Fatalf("ErrorLines mismatch: %+v", got.Summary.ErrorLines)
	}
}

// TestUpdateAdvertisedInfo_SummaryIsDeepCopied: a later mutation on the
// inbound ANNOUNCE slice must not leak into the mirror session's meta.
func TestUpdateAdvertisedInfo_SummaryIsDeepCopied(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})

	lines := []string{"a", "b"}
	s.UpdateAdvertisedInfo(proto.SessionInfo{
		ID:      s.ID.String(),
		Summary: &proto.SessionSummary{RecentOutput: "x", ErrorLines: lines},
	})

	// Mutate the caller's slice after the update.
	lines[0] = "MUTATED"

	got := s.Info()
	if got.Summary.ErrorLines[0] != "a" {
		t.Fatalf("ErrorLines[0] = %q after caller mutation, want %q (deep copy broken)", got.Summary.ErrorLines[0], "a")
	}
}

// TestUpdateAdvertisedInfo_SameSummaryNoBroadcast: re-announcing an
// identical Summary should be a no-op (no spurious META broadcast).
// We cannot easily observe broadcasts without subscribers; instead
// verify the helper sameSummary returns true for equivalent pointers.
func TestSameSummary_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		a, b *proto.SessionSummary
		want bool
	}{
		{"both nil", nil, nil, true},
		{"a nil only", nil, &proto.SessionSummary{}, false},
		{"b nil only", &proto.SessionSummary{}, nil, false},
		{"empty equal", &proto.SessionSummary{}, &proto.SessionSummary{}, true},
		{
			"identical content",
			&proto.SessionSummary{RecentOutput: "x", ErrorLines: []string{"e"}, CapturedAt: 1},
			&proto.SessionSummary{RecentOutput: "x", ErrorLines: []string{"e"}, CapturedAt: 1},
			true,
		},
		{
			"different RecentOutput",
			&proto.SessionSummary{RecentOutput: "x"},
			&proto.SessionSummary{RecentOutput: "y"},
			false,
		},
		{
			"different CapturedAt",
			&proto.SessionSummary{CapturedAt: 1},
			&proto.SessionSummary{CapturedAt: 2},
			false,
		},
		{
			"different ErrorLines length",
			&proto.SessionSummary{ErrorLines: []string{"a"}},
			&proto.SessionSummary{ErrorLines: []string{"a", "b"}},
			false,
		},
		{
			"different ErrorLines content",
			&proto.SessionSummary{ErrorLines: []string{"a"}},
			&proto.SessionSummary{ErrorLines: []string{"b"}},
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameSummary(tc.a, tc.b); got != tc.want {
				t.Fatalf("sameSummary = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestUpdateAdvertisedInfo_OpaqueSessionStillTakesSummary: the M2c
// contentOpaque flag suppresses OUT-byte parsing but must NOT suppress
// ANNOUNCE-driven Summary adoption. The agent owns Summary; the relay
// just stores what the agent advertises.
func TestUpdateAdvertisedInfo_OpaqueSessionStillTakesSummary(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	s.MarkContentOpaque()

	s.UpdateAdvertisedInfo(proto.SessionInfo{
		ID:      s.ID.String(),
		Summary: &proto.SessionSummary{RecentOutput: "agent-side-derived"},
	})

	if got := s.Info().Summary; got == nil || got.RecentOutput != "agent-side-derived" {
		t.Fatalf("Summary not adopted on opaque session: %+v", got)
	}
}
