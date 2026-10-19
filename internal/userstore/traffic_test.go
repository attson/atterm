package userstore

import (
	"context"
	"testing"
	"time"
)

func TestTrafficBucketFailureRollsBackDailyAndMinuteTotals(t *testing.T) {
	store := NewInMemory(t)
	ctx := context.Background()
	_, err := store.db.ExecContext(ctx, `CREATE TRIGGER fail_hour BEFORE INSERT ON relay_traffic_buckets
 WHEN NEW.bucket = 'hour' BEGIN SELECT RAISE(ABORT, 'simulated failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Minute)
	day := now.Format("2006-01-02")
	if err := store.AddTrafficDeltas(ctx, day, []TrafficDelta{{UserID: "u1", Minute: now.Unix(), Bytes: 100, Frames: 1}}); err == nil {
		t.Fatal("expected hour write failure")
	}
	daily, err := store.QueryTraffic(ctx, day, day)
	if err != nil || len(daily) != 0 {
		t.Fatalf("partial daily commit: %+v %v", daily, err)
	}
	minute, err := store.QueryTrafficBuckets(ctx, "u1", "minute", now.Unix(), now.Add(time.Minute).Unix())
	if err != nil || len(minute) != 0 {
		t.Fatalf("partial minute commit: %+v %v", minute, err)
	}
}
