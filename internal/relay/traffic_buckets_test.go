package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/userstore"
)

func trafficBucketURL(path, bucket string, from, to time.Time) string {
	return path + "?" + url.Values{"bucket": {bucket}, "from": {from.Format(time.RFC3339)}, "to": {to.Format(time.RFC3339)}}.Encode()
}

func TestTrafficBucketsPersonalIsolationAndSharedAdminTotals(t *testing.T) {
	srv, store, userID, token := newAdminTestServer(t)
	start := time.Now().UTC().Truncate(time.Minute)
	if err := store.AddTrafficDeltas(context.Background(), start.Format("2006-01-02"), []userstore.TrafficDelta{
		{UserID: userID, Minute: start.Unix(), FrameType: int(proto.TypeOut), Direction: trafficOut, Bytes: 120, Frames: 3},
		{UserID: "another-user", Minute: start.Unix(), FrameType: int(proto.TypeOut), Direction: trafficOut, Bytes: 999, Frames: 7},
	}); err != nil {
		t.Fatal(err)
	}
	for _, bucket := range []string{"minute", "hour"} {
		from := start.Truncate(time.Hour)
		to := start.Add(time.Minute)
		personal := adminGetBearer(srv, trafficBucketURL("/api/me/traffic", bucket, from, to)+"&user_id=another-user", token)
		if personal.Code != http.StatusOK {
			t.Fatalf("personal status=%d: %s", personal.Code, personal.Body.String())
		}
		var me struct {
			RelayDetail   []meRelayTrafficDetail `json:"relay_detail"`
			BucketSeconds int                    `json:"bucket_seconds"`
			UpdatedAt     string                 `json:"updated_at"`
		}
		if err := json.Unmarshal(personal.Body.Bytes(), &me); err != nil {
			t.Fatal(err)
		}
		if len(me.RelayDetail) != 1 || me.RelayDetail[0].Bytes != 120 || me.BucketSeconds == 0 || me.UpdatedAt == "" {
			t.Fatalf("personal = %s", personal.Body.String())
		}
		if strings.Contains(personal.Body.String(), "user_id") || strings.Contains(personal.Body.String(), "another-user") {
			t.Fatal("personal response leaked identity")
		}
		admin := adminGetBearer(srv, trafficBucketURL("/admin/api/traffic", bucket, from, to), token)
		if admin.Code != http.StatusOK {
			t.Fatalf("admin status=%d: %s", admin.Code, admin.Body.String())
		}
		var result trafficResponse
		if err := json.Unmarshal(admin.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		var total int64
		for _, row := range result.Rows {
			total += row.Bytes
		}
		if total != 1119 {
			t.Fatalf("admin total=%d", total)
		}
	}
}

func TestTrafficBucketsReadsDoNotFlushAndAcceptToday(t *testing.T) {
	srv, _, userID, token := newAdminTestServer(t)
	now := time.Now().UTC()
	start := now.Truncate(24 * time.Hour)
	srv.recordTraffic(userID, proto.TypeOut, trafficOut, 73)
	path := trafficBucketURL("/api/me/traffic", "minute", start, start.Add(24*time.Hour))
	rec := adminGetBearer(srv, path, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("today status=%d: %s", rec.Code, rec.Body.String())
	}
	var response meTrafficResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.RelayDetail) != 0 {
		t.Fatal("read unexpectedly flushed pending traffic")
	}
	srv.flushTraffic(srv.cfg.Store)
	rec = adminGetBearer(srv, path, token)
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.RelayDetail) != 1 || response.RelayDetail[0].Bytes != 73 {
		t.Fatalf("flushed response=%s", rec.Body.String())
	}
}

func TestTrafficBucketsInvalidRangesAndAuthentication(t *testing.T) {
	srv, _, _, token := newAdminTestServer(t)
	start := time.Now().UTC().Truncate(24 * time.Hour)
	for _, path := range []string{"/api/me/traffic", "/admin/api/traffic"} {
		valid := trafficBucketURL(path, "minute", start, start.Add(24*time.Hour))
		if got := adminGetBearer(srv, valid, ""); got.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized status=%d", got.Code)
		}
		for _, query := range []string{
			trafficBucketURL(path, "minute", start.AddDate(0, 0, -8), start),
			trafficBucketURL(path, "hour", start.AddDate(0, 0, -91), start),
			trafficBucketURL(path, "minute", start.AddDate(0, 0, -3), start),
			trafficBucketURL(path, "hour", start, start),
			trafficBucketURL(path, "hour", start.Add(time.Hour), start),
			trafficBucketURL(path, "hour", start, start.AddDate(0, 0, 2)),
			path + "?bucket=minute&from=bad&to=bad",
		} {
			if got := adminGetBearer(srv, query, token); got.Code != http.StatusBadRequest {
				t.Fatalf("%s status=%d", query, got.Code)
			}
		}
	}
}
