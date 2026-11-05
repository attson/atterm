package userstore

import (
	"context"
	"path/filepath"
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

func TestTrafficQuarterAndDirectBucketsAreAbsoluteAndAdditive(t *testing.T) {
	store := NewInMemory(t)
	ctx := context.Background()
	startedAt := time.Date(2026, 11, 5, 1, 7, 0, 0, time.UTC)
	for range 2 {
		if err := store.AddTrafficDeltas(ctx, startedAt.Format(time.DateOnly), []TrafficDelta{{
			UserID: "u1", Minute: startedAt.Unix(), Bytes: 10, Frames: 1,
		}}); err != nil {
			t.Fatal(err)
		}
		if err := store.AddDirectTrafficDeltas(ctx, startedAt.Format(time.DateOnly), []DirectTrafficDelta{{
			UserID: "u1", StartedAt: startedAt.Unix(), Attempts: 1, BytesSent: 20,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	quarter := startedAt.Unix() / 900 * 900
	relayRows, err := store.QueryTrafficBuckets(ctx, "u1", "quarter", quarter, quarter+900)
	if err != nil || len(relayRows) != 1 || relayRows[0].Bytes != 20 || relayRows[0].Frames != 2 {
		t.Fatalf("relay quarter=%+v err=%v", relayRows, err)
	}
	directRows, err := store.QueryDirectTrafficBuckets(ctx, "u1", quarter, quarter+900)
	if err != nil || len(directRows) != 1 || directRows[0].Attempts != 2 || directRows[0].BytesSent != 40 {
		t.Fatalf("direct quarter=%+v err=%v", directRows, err)
	}
}

func TestTrafficClientMigrationPreservesLegacyTotals(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "users.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.ExecContext(ctx, `
		DROP TABLE relay_traffic_daily;
		CREATE TABLE relay_traffic_daily (
			user_id TEXT NOT NULL, day TEXT NOT NULL, frame_type INTEGER NOT NULL,
			direction INTEGER NOT NULL, bytes BIGINT NOT NULL DEFAULT 0,
			frames BIGINT NOT NULL DEFAULT 0, updated_at BIGINT NOT NULL,
			PRIMARY KEY (user_id, day, frame_type, direction));
		CREATE INDEX idx_relay_traffic_day ON relay_traffic_daily (day);
		INSERT INTO relay_traffic_daily VALUES ('u1', '2026-10-20', 3, 1, 123, 4, 99);
		DROP TABLE relay_traffic_buckets;
		CREATE TABLE relay_traffic_buckets (
			user_id TEXT NOT NULL, bucket TEXT NOT NULL, started_at BIGINT NOT NULL,
			frame_type INTEGER NOT NULL, direction INTEGER NOT NULL,
			bytes BIGINT NOT NULL DEFAULT 0, frames BIGINT NOT NULL DEFAULT 0,
			updated_at BIGINT NOT NULL,
			PRIMARY KEY (user_id, bucket, started_at, frame_type, direction));
		CREATE INDEX idx_traffic_buckets_time ON relay_traffic_buckets (bucket, started_at);
		INSERT INTO relay_traffic_buckets VALUES ('u1', 'minute', 1792497600, 3, 1, 123, 4, 99);
		DELETE FROM schema_migrations WHERE name = '0015_traffic_clients.sql';`)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	daily, err := store.QueryTrafficForUser(ctx, "u1", "2026-10-20", "2026-10-20")
	if err != nil || len(daily) != 1 || daily[0].Bytes != 123 || daily[0].Frames != 4 || daily[0].ClientID != "" {
		t.Fatalf("migrated daily=%+v err=%v", daily, err)
	}
	buckets, err := store.QueryTrafficBuckets(ctx, "u1", "minute", 1792497600, 1792497660)
	if err != nil || len(buckets) != 1 || buckets[0].Bytes != 123 || buckets[0].Frames != 4 || buckets[0].ClientID != "" {
		t.Fatalf("migrated buckets=%+v err=%v", buckets, err)
	}
}
