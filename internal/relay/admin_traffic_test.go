package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/userstore"
)

// seedTraffic folds a couple of rollup rows for today so the handler has
// something to aggregate.
func seedTraffic(t *testing.T, store *userstore.DBStore, userID string) string {
	t.Helper()
	day := time.Now().UTC().Format("2006-01-02")
	if err := store.AddTrafficDeltas(context.Background(), day, []userstore.TrafficDelta{
		{UserID: userID, FrameType: int(proto.TypeOut), Direction: trafficOut, Bytes: 1000, Frames: 10},
		{UserID: userID, FrameType: int(proto.TypeIn), Direction: trafficIn, Bytes: 200, Frames: 4},
		{UserID: userID, FrameType: int(proto.TypeMeta), Direction: trafficOut, Bytes: 300, Frames: 3},
	}); err != nil {
		t.Fatalf("seed traffic: %v", err)
	}
	return day
}

func TestAdminTraffic_GroupView(t *testing.T) {
	srv, store, adminID, tok := newAdminTestServer(t)
	day := seedTraffic(t, store, adminID)

	rec := adminGetBearer(srv, "/admin/api/traffic?view=group&from="+day+"&to="+day, tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		View string `json:"view"`
		Rows []struct {
			Category  string `json:"category"`
			Direction int    `json:"direction"`
			Bytes     int64  `json:"bytes"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.View != "group" {
		t.Errorf("view = %q, want group", resp.View)
	}
	// TypeOut + TypeMeta OUT fold into terminal(1000) + state(300) OUT rows.
	var terminalOut, stateOut int64
	for _, r := range resp.Rows {
		if r.Direction == trafficOut && r.Category == "terminal" {
			terminalOut = r.Bytes
		}
		if r.Direction == trafficOut && r.Category == "state" {
			stateOut = r.Bytes
		}
	}
	if terminalOut != 1000 {
		t.Errorf("terminal OUT = %d, want 1000", terminalOut)
	}
	if stateOut != 300 {
		t.Errorf("state OUT = %d, want 300", stateOut)
	}
}

func TestAdminTraffic_DetailView(t *testing.T) {
	srv, store, adminID, tok := newAdminTestServer(t)
	day := seedTraffic(t, store, adminID)

	rec := adminGetBearer(srv, "/admin/api/traffic?view=detail&from="+day+"&to="+day, tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp struct {
		Rows []struct {
			FrameType     int    `json:"frame_type"`
			FrameTypeName string `json:"frame_type_name"`
			Category      string `json:"category"`
			Direction     int    `json:"direction"`
			Bytes         int64  `json:"bytes"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var outBytes int64
	for _, r := range resp.Rows {
		if r.FrameType == int(proto.TypeOut) && r.Direction == trafficOut {
			outBytes = r.Bytes
			if r.FrameTypeName == "" {
				t.Error("detail row missing frame_type_name")
			}
			if r.Category != "terminal" {
				t.Errorf("detail row category = %q, want terminal", r.Category)
			}
		}
	}
	if outBytes != 1000 {
		t.Errorf("OUT frame bytes = %d, want 1000", outBytes)
	}
}

func TestAdminTraffic_DayBucketKeepsDaysSeparate(t *testing.T) {
	srv, store, adminID, tok := newAdminTestServer(t)
	today := time.Now().UTC()
	from := today.AddDate(0, 0, -1).Format("2006-01-02")
	to := today.Format("2006-01-02")
	for _, day := range []string{from, to} {
		if err := store.AddTrafficDeltas(context.Background(), day, []userstore.TrafficDelta{
			{UserID: adminID, FrameType: int(proto.TypeOut), Direction: trafficOut, Bytes: 100, Frames: 1},
		}); err != nil {
			t.Fatalf("seed traffic for %s: %v", day, err)
		}
	}

	rec := adminGetBearer(srv, "/admin/api/traffic?view=detail&bucket=day&from="+from+"&to="+to, tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Bucket string `json:"bucket"`
		Rows   []struct {
			Day   string `json:"day"`
			Bytes int64  `json:"bytes"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Bucket != "day" {
		t.Errorf("bucket = %q, want day", resp.Bucket)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(resp.Rows))
	}
	if resp.Rows[0].Day != from || resp.Rows[1].Day != to {
		t.Errorf("days = [%q, %q], want [%q, %q]", resp.Rows[0].Day, resp.Rows[1].Day, from, to)
	}
}

func TestAdminTraffic_SummaryView(t *testing.T) {
	srv, store, adminID, tok := newAdminTestServer(t)
	day := seedTraffic(t, store, adminID)

	rec := adminGetBearer(srv, "/admin/api/traffic?view=summary&from="+day+"&to="+day, tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp struct {
		Rows []struct {
			Direction int   `json:"direction"`
			Bytes     int64 `json:"bytes"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Summary collapses OUT rows: TypeOut(1000) + TypeMeta(300) = 1300.
	var out int64
	for _, r := range resp.Rows {
		if r.Direction == trafficOut {
			out += r.Bytes
		}
	}
	if out != 1300 {
		t.Errorf("summary OUT total = %d, want 1300", out)
	}
}

func TestAdminTraffic_InvalidDate(t *testing.T) {
	srv, _, _, tok := newAdminTestServer(t)
	rec := adminGetBearer(srv, "/admin/api/traffic?from=not-a-date", tok)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAdminTraffic_InvalidView(t *testing.T) {
	srv, _, _, tok := newAdminTestServer(t)
	rec := adminGetBearer(srv, "/admin/api/traffic?view=bogus", tok)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAdminTraffic_InvalidBucket(t *testing.T) {
	srv, _, _, tok := newAdminTestServer(t)
	rec := adminGetBearer(srv, "/admin/api/traffic?bucket=hour", tok)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAdminTraffic_ReversedDateRange(t *testing.T) {
	srv, _, _, tok := newAdminTestServer(t)
	rec := adminGetBearer(srv, "/admin/api/traffic?from=2026-09-21&to=2026-09-20", tok)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAdminTraffic_RejectsNonAdmin(t *testing.T) {
	srv, store, _, _ := newAdminTestServer(t)
	// Create a second, non-admin user with its own session on the same store.
	ctx := context.Background()
	u, err := store.CreateOpaqueUser(ctx, "plain@example.com")
	if err != nil {
		t.Fatalf("CreateOpaqueUser: %v", err)
	}
	userTok, _, err := store.CreateSession(ctx, u.ID, "ua", "203.0.113.0/24", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	rec := adminGetBearer(srv, "/admin/api/traffic", userTok)
	if rec.Code == http.StatusOK {
		t.Errorf("non-admin got 200, want 401/403; code=%d", rec.Code)
	}
}
