package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/userstore"
)

func TestMeTraffic_OnlyReturnsAuthenticatedUser(t *testing.T) {
	srv, store, userID, token := newAdminTestServer(t)
	day := time.Now().UTC().Format("2006-01-02")
	other, err := store.CreateOpaqueUser(context.Background(), "other@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddTrafficDeltas(context.Background(), day, []userstore.TrafficDelta{
		{UserID: userID, FrameType: int(proto.TypeOut), Direction: trafficOut, Bytes: 1200, Frames: 3},
		{UserID: userID, FrameType: int(proto.TypeIn), Direction: trafficIn, Bytes: 300, Frames: 2},
		{UserID: other.ID, FrameType: int(proto.TypeOut), Direction: trafficOut, Bytes: 9999, Frames: 9},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddDirectTrafficDeltas(context.Background(), day, []userstore.DirectTrafficDelta{
		{UserID: userID, Attempts: 4, Successes: 3, Fallbacks: 1, BytesSent: 800, BytesReceived: 200},
		{UserID: other.ID, Attempts: 9, Successes: 9, BytesSent: 9999},
	}); err != nil {
		t.Fatal(err)
	}

	rec := adminGetBearer(srv, "/api/me/traffic?from="+day+"&to="+day, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var got meTrafficResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Relay) != 1 || got.Relay[0].BytesOut != 1200 || got.Relay[0].BytesIn != 300 {
		t.Fatalf("relay = %#v", got.Relay)
	}
	if len(got.RelayDetail) != 2 || got.RelayDetail[0].FrameTypeName == "" || got.RelayDetail[0].Category == "" {
		t.Fatalf("relay detail = %#v", got.RelayDetail)
	}
	if len(got.Direct) != 1 || got.Direct[0].BytesSent != 800 || got.Direct[0].BytesReceived != 200 || got.Direct[0].Successes != 3 {
		t.Fatalf("direct = %#v", got.Direct)
	}
	if strings.Contains(rec.Body.String(), other.ID) || strings.Contains(rec.Body.String(), "other@example.com") || strings.Contains(rec.Body.String(), userID) {
		t.Fatalf("personal response leaked identity fields: %s", rec.Body.String())
	}
}

func TestMeTraffic_RequiresAuthenticationAndValidRange(t *testing.T) {
	srv, _, _, token := newAdminTestServer(t)
	if rec := adminGetBearer(srv, "/api/me/traffic", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", rec.Code)
	}
	for _, path := range []string{
		"/api/me/traffic?from=bad",
		"/api/me/traffic?from=2026-09-23&to=2026-09-22",
		"/api/me/traffic?from=2026-01-01&to=2026-09-23",
	} {
		if rec := adminGetBearer(srv, path, token); rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", path, rec.Code)
		}
	}
}

func TestMeTraffic_FlushesPendingMetricsBeforeQuery(t *testing.T) {
	srv, _, userID, token := newAdminTestServer(t)
	day := time.Now().UTC().Format("2006-01-02")
	srv.recordTraffic(userID, proto.TypeOut, trafficOut, 73)
	srv.directStats.recordAttempt(userID)
	srv.directStats.recordSuccess(userID)
	srv.directStats.recordBytes(userID, 123, 45)

	rec := adminGetBearer(srv, "/api/me/traffic?from="+day+"&to="+day, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var got meTrafficResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Relay) != 1 || got.Relay[0].BytesOut != 73 {
		t.Fatalf("relay = %#v", got.Relay)
	}
	if len(got.Direct) != 1 || got.Direct[0].Attempts != 1 || got.Direct[0].Successes != 1 || got.Direct[0].BytesSent != 123 || got.Direct[0].BytesReceived != 45 {
		t.Fatalf("direct = %#v", got.Direct)
	}
}
