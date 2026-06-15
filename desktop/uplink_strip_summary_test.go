package main

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/attson/atterm/internal/proto"
)

// TestStripContentFieldsFromSnapshot_DropsSummary ensures that the M3a
// strip is surgical: Summary becomes nil, everything else survives.
// Structural metadata (id, host, cols/rows, started_at, task_state,
// etc.) must remain so the relay can still route, schedule push
// timing, and order the session list.
func TestStripContentFieldsFromSnapshot_DropsSummary(t *testing.T) {
	in := []proto.SessionInfo{
		{
			ID:                "11111111-1111-1111-1111-111111111111",
			HostID:            "host-1",
			Host:              "alice-laptop",
			User:              "alice",
			Title:             "atterm - bash",
			Cwd:               "/Users/alice/code",
			Command:           "bash",
			CurrentCommand:    "go test ./...",
			Cols:              80,
			Rows:              24,
			StartedAt:         1700000000,
			LastOutputAt:      1700000050,
			TaskState:         proto.TaskStateRunning,
			CommandStartedAt:  1700000020,
			CommandDurationMS: 0,
			Summary: &proto.SessionSummary{
				RecentOutput: "$ go test ./...\nok  pkg  0.123s\n$ ",
				ErrorLines:   []string{"FAIL: TestFoo"},
				CapturedAt:   1700000050,
			},
			AttentionAt: 1700000050,
		},
	}

	out := stripContentFieldsFromSnapshot(in)

	if got := out[0].Summary; got != nil {
		t.Fatalf("Summary not stripped: %+v", got)
	}
	// Structural fields preserved.
	if out[0].ID != in[0].ID ||
		out[0].HostID != in[0].HostID ||
		out[0].Cols != in[0].Cols ||
		out[0].StartedAt != in[0].StartedAt ||
		out[0].TaskState != in[0].TaskState ||
		out[0].LastOutputAt != in[0].LastOutputAt ||
		out[0].AttentionAt != in[0].AttentionAt {
		t.Fatalf("structural metadata mutated: %+v", out[0])
	}
	// Semi-sensitive fields stay for M3a — M3b will fold them into a sealed envelope.
	if out[0].Title != in[0].Title ||
		out[0].Cwd != in[0].Cwd ||
		out[0].Command != in[0].Command ||
		out[0].CurrentCommand != in[0].CurrentCommand {
		t.Fatalf("M3a kept title/cwd/command in plaintext; got %+v", out[0])
	}
}

// TestStripContentFieldsFromSnapshot_NoMutationOfInput: the helper must
// return a fresh slice so a future ANNOUNCE re-snapshot still has
// Summary in its in-memory mirror copy. (Agent's local renderer still
// shows Summary for its own host.)
func TestStripContentFieldsFromSnapshot_NoMutationOfInput(t *testing.T) {
	in := []proto.SessionInfo{
		{ID: "x", Summary: &proto.SessionSummary{RecentOutput: "keep-me"}},
	}
	_ = stripContentFieldsFromSnapshot(in)
	if in[0].Summary == nil || in[0].Summary.RecentOutput != "keep-me" {
		t.Fatalf("input mutated: %+v", in[0].Summary)
	}
}

func TestStripMetaSummary_DropsSummary(t *testing.T) {
	payload, _ := json.Marshal(proto.MetaPayload{
		Title:     "t",
		Cwd:       "/x",
		TaskState: proto.TaskStateRunning,
		Cols:      80,
		Summary:   &proto.SessionSummary{RecentOutput: "secret"},
	})
	f := proto.Frame{Type: proto.TypeMeta, SessionID: uuid.New(), Payload: payload}
	out, ok := stripMetaSummary(f)
	if !ok {
		t.Fatalf("stripMetaSummary returned ok=false")
	}
	var got proto.MetaPayload
	if err := json.Unmarshal(out.Payload, &got); err != nil {
		t.Fatalf("decode stripped: %v", err)
	}
	if got.Summary != nil {
		t.Fatalf("Summary still present: %+v", got.Summary)
	}
	if got.Title != "t" || got.Cwd != "/x" || got.Cols != 80 || got.TaskState != proto.TaskStateRunning {
		t.Fatalf("other fields mutated: %+v", got)
	}
}

func TestStripMetaSummary_NoSummary_NoOp(t *testing.T) {
	payload, _ := json.Marshal(proto.MetaPayload{Title: "t"})
	f := proto.Frame{Type: proto.TypeMeta, SessionID: uuid.New(), Payload: payload}
	_, ok := stripMetaSummary(f)
	if ok {
		t.Fatalf("expected ok=false when Summary is already nil")
	}
}

func TestStripMetaSummary_BadJSON_NoOp(t *testing.T) {
	f := proto.Frame{Type: proto.TypeMeta, SessionID: uuid.New(), Payload: []byte("not json")}
	out, ok := stripMetaSummary(f)
	if ok {
		t.Fatalf("expected ok=false on bad JSON")
	}
	if string(out.Payload) != "not json" {
		t.Fatalf("bad-JSON path mutated payload")
	}
}
