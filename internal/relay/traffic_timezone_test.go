package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/userstore"
)

func TestParseTrafficDayRangeHandlesDSTAndQuarterHourZones(t *testing.T) {
	tests := []struct {
		day, zone string
		want      time.Duration
		wantStart string
	}{
		{day: "2026-03-08", zone: "America/Los_Angeles", want: 23 * time.Hour},
		{day: "2026-11-01", zone: "America/Los_Angeles", want: 25 * time.Hour},
		{day: "2026-11-05", zone: "Asia/Kathmandu", want: 24 * time.Hour, wantStart: "2026-11-04T18:15:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.zone+tt.day, func(t *testing.T) {
			rangeValue, err := parseTrafficDayRange(tt.day, tt.day, tt.zone, 2)
			if err != nil {
				t.Fatal(err)
			}
			if got := rangeValue.to.Sub(rangeValue.from); got != tt.want {
				t.Fatalf("duration=%s want=%s", got, tt.want)
			}
			if tt.wantStart != "" && rangeValue.from.UTC().Format(time.RFC3339) != tt.wantStart {
				t.Fatalf("start=%s want=%s", rangeValue.from.UTC().Format(time.RFC3339), tt.wantStart)
			}
		})
	}
	if _, err := parseTrafficDayRange("2026-01-01", "2026-01-02", "Not/AZone", 2); err == nil {
		t.Fatal("invalid zone accepted")
	}
}

func TestMeTrafficTimeZoneAggregatesAbsoluteBuckets(t *testing.T) {
	srv, store, userID, token := newAdminTestServer(t)
	startedAt := time.Now().UTC().Truncate(15 * time.Minute)
	day := startedAt.In(mustLocation(t, "Asia/Shanghai")).Format(time.DateOnly)
	if err := store.AddTrafficDeltas(context.Background(), startedAt.UTC().Format(time.DateOnly), []userstore.TrafficDelta{
		{Minute: startedAt.Unix(), UserID: userID, FrameType: int(proto.TypeOut), Direction: trafficOut, Bytes: 321, Frames: 3},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddDirectTrafficDeltas(context.Background(), startedAt.UTC().Format(time.DateOnly), []userstore.DirectTrafficDelta{
		{StartedAt: startedAt.Unix(), UserID: userID, Attempts: 1, Successes: 1, BytesSent: 45},
	}); err != nil {
		t.Fatal(err)
	}
	query := url.Values{"from": {day}, "to": {day}, "tz": {"Asia/Shanghai"}}
	rec := adminGetBearer(srv, "/api/me/traffic?"+query.Encode(), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got meTrafficResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.TimeZone != "Asia/Shanghai" || got.DayBasis != "time_zone" || got.LocalCoverageFrom == "" {
		t.Fatalf("metadata=%+v", got)
	}
	if len(got.Relay) != 1 || got.Relay[0].Day != day || got.Relay[0].BytesOut != 321 {
		t.Fatalf("relay=%+v", got.Relay)
	}
	if len(got.Direct) != 1 || got.Direct[0].Day != day || got.Direct[0].BytesSent != 45 {
		t.Fatalf("direct=%+v", got.Direct)
	}
	bad := adminGetBearer(srv, "/api/me/traffic?from="+day+"&to="+day+"&tz=Not%2FAZone", token)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid zone status=%d", bad.Code)
	}
}

func TestAggregateTrafficBucketsAlignsToReportingMidnight(t *testing.T) {
	origin := time.Date(2026, 11, 4, 18, 15, 0, 0, time.UTC).Unix()
	rows := []userstore.TrafficRow{
		{Day: "2026-11-04T18:15:00Z", UserID: "u1", FrameType: int(proto.TypeOut), Direction: trafficOut, Bytes: 10, Frames: 1},
		{Day: "2026-11-04T19:00:00Z", UserID: "u1", FrameType: int(proto.TypeOut), Direction: trafficOut, Bytes: 20, Frames: 2},
		{Day: "2026-11-04T19:15:00Z", UserID: "u1", FrameType: int(proto.TypeOut), Direction: trafficOut, Bytes: 30, Frames: 3},
	}
	got := aggregateTrafficBuckets(rows, origin, 3600)
	if len(got) != 2 || got[0].Day != "2026-11-04T18:15:00Z" || got[0].Bytes != 30 || got[1].Day != "2026-11-04T19:15:00Z" || got[1].Bytes != 30 {
		t.Fatalf("aggregated=%+v", got)
	}
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return location
}
