package session

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestUpdateSizeChangesSessionInfo(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	s.UpdateSize(132, 43)

	info := s.Info()
	if info.Cols != 132 || info.Rows != 43 {
		t.Fatalf("size = %dx%d; want 132x43", info.Cols, info.Rows)
	}
}
