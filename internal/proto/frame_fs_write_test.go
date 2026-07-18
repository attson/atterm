package proto

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestFSRequestPayloadWriteFieldsRoundTrip(t *testing.T) {
	req := FSRequestPayload{
		RequestID:       "r1",
		Op:              "write_file",
		Path:            "/a/b.txt",
		Data:            []byte("hi"),
		ExpectedModTime: 1234,
		CreateIfMissing: true,
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back FSRequestPayload
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.RequestID != req.RequestID || back.Op != req.Op || back.Path != req.Path ||
		back.ExpectedModTime != req.ExpectedModTime || back.CreateIfMissing != req.CreateIfMissing ||
		!bytes.Equal(back.Data, req.Data) {
		t.Fatalf("round-trip lost fields: %+v", back)
	}
}

func TestFSRequestPayloadRenameRoundTrip(t *testing.T) {
	req := FSRequestPayload{Op: "rename", Path: "/a", NewPath: "/b"}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back FSRequestPayload
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Op != "rename" || back.Path != "/a" || back.NewPath != "/b" {
		t.Fatalf("round-trip lost fields: %+v", back)
	}
}

func TestFSRequestPayloadRemoveRoundTrip(t *testing.T) {
	req := FSRequestPayload{Op: "remove", Path: "/a/b", Recursive: true}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back FSRequestPayload
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Op != "remove" || back.Path != "/a/b" || !back.Recursive {
		t.Fatalf("round-trip lost fields: %+v", back)
	}
}

func TestFSResponsePayloadMetaOnWrite(t *testing.T) {
	resp := FSResponsePayload{
		RequestID: "r1",
		OK:        true,
		Meta:      &FileMetaInfo{Path: "/a.txt", Size: 2, ModTime: 5678},
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back FSResponsePayload
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.OK || back.RequestID != "r1" || back.Meta == nil ||
		back.Meta.Path != "/a.txt" || back.Meta.Size != 2 || back.Meta.ModTime != 5678 {
		t.Fatalf("round-trip lost fields: %+v", back)
	}
}
