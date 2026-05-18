// proto-fixtures emits canonical binary frames for every protocol type
// the web client speaks. The TS protocol.test.ts round-trips against
// these to verify wire compatibility (AGENTS.md red-line 4).
//
// Regenerate after any internal/proto wire change:
//
//	go run ./cmd/proto-fixtures
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func main() {
	outDir := filepath.Join("web", "tests", "fixtures", "proto")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	sid := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	emit := func(name string, f proto.Frame) {
		path := filepath.Join(outDir, name)
		if err := os.WriteFile(path, proto.Marshal(f), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	emit("out.bin", proto.EncodeOut(sid, 42, []byte("hello\r\n")))
	emit("resize.bin", proto.Frame{
		Type: proto.TypeResize, SessionID: sid, Payload: proto.EncodeResize(80, 24),
	})
	emit("attach.bin", proto.Frame{
		Type:      proto.TypeAttach,
		SessionID: sid,
		Payload: mustMarshal(proto.AttachPayload{
			SessionID: sid.String(), SinceSeq: 100, ClientID: "client-uuid", ClientName: "laptop",
		}),
	})
	emit("meta.bin", proto.Frame{
		Type:      proto.TypeMeta,
		SessionID: sid,
		Payload: mustMarshal(proto.MetaPayload{
			Cwd: "/home/me", Title: "demo", Cols: 80, Rows: 24,
		}),
	})
	emit("close.bin", proto.Frame{
		Type:      proto.TypeClose,
		SessionID: sid,
		Payload:   mustMarshal(proto.ClosePayload{ExitCode: 0, Reason: ""}),
	})
	emit("replay-start.bin", proto.Frame{
		Type:      proto.TypeReplayProgress,
		SessionID: sid,
		Payload: mustMarshal(proto.ReplayProgressPayload{
			Phase: proto.ReplayProgressStart, Bytes: 0, TotalBytes: 12345,
		}),
	})
	emit("paste-image.bin", proto.Frame{
		Type:      proto.TypePasteImage,
		SessionID: sid,
		Payload: mustMarshal(proto.PasteImagePayload{
			Filename: "shot.png", ContentType: "image/png", Data: []byte{1, 2, 3, 4},
		}),
	})
	emit("list.bin", proto.Frame{Type: proto.TypeList})  // no session id, no payload
	emit("ping.bin", proto.Frame{Type: proto.TypePing})  // empty frame
}
