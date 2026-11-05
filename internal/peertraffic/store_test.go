package peertraffic

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStorePersistsAndMergesLocalRouteTraffic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer-traffic.json")
	now := time.Date(2026, 10, 9, 23, 0, 0, 0, time.FixedZone("test", 8*60*60))
	first := New(path, func() time.Time { return now })
	second := New(path, func() time.Time { return now })

	first.Record(RouteDirect, DirectionSent, 100)
	first.Record(RouteDirect, DirectionReceived, 70)
	second.Record(RouteDirect, DirectionSent, 25)
	second.Record(RouteLAN, DirectionReceived, 300)
	if err := first.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := second.Flush(); err != nil {
		t.Fatal(err)
	}

	reopened := New(path, func() time.Time { return now })
	rows, err := reopened.Query(now.AddDate(0, 0, -1), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%+v", rows)
	}
	if got := rows[0]; got.Route != RouteDirect || got.BytesSent != 125 || got.BytesReceived != 70 || got.RecordsSent != 2 || got.RecordsReceived != 1 {
		t.Fatalf("direct row=%+v", got)
	}
	if got := rows[1]; got.Route != RouteLAN || got.BytesReceived != 300 || got.RecordsReceived != 1 {
		t.Fatalf("LAN row=%+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("store mode=%v", info.Mode().Perm())
	}
}

func TestStoreConcurrentRecordAndRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer-traffic.json")
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	store := New(path, func() time.Time { return now })
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Record(RouteQuickTunnel, DirectionSent, 10)
		}()
	}
	wg.Wait()
	rows, err := store.Query(now, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].BytesSent != 1000 || rows[0].RecordsSent != 100 {
		t.Fatalf("rows=%+v", rows)
	}
	if outside, err := store.Query(now.AddDate(0, 0, 1), now.AddDate(0, 0, 1)); err != nil || len(outside) != 0 {
		t.Fatalf("outside rows=%+v err=%v", outside, err)
	}
}

func TestStoreConcurrentInstancesMergeWithoutLostUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer-traffic.json")
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	first := New(path, func() time.Time { return now })
	second := New(path, func() time.Time { return now })
	for range 100 {
		first.Record(RouteDirect, DirectionSent, 3)
		second.Record(RouteDirect, DirectionReceived, 5)
	}
	start := make(chan struct{})
	errors := make(chan error, 2)
	for _, store := range []*Store{first, second} {
		go func() {
			<-start
			errors <- store.Flush()
		}()
	}
	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := New(path, func() time.Time { return now }).Query(now, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].BytesSent != 300 || rows[0].RecordsSent != 100 ||
		rows[0].BytesReceived != 500 || rows[0].RecordsReceived != 100 {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestStoreFailedFlushRetainsPendingTraffic(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "peer-traffic.json")
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	store := New(path, func() time.Time { return now })
	store.Record(RouteGateway, DirectionReceived, 42)
	if err := os.WriteFile(path, []byte(`{"version":1,"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Flush(); err == nil {
		t.Fatal("corrupt store accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Query(now, now)
	if err != nil || len(rows) != 1 || rows[0].BytesReceived != 42 {
		t.Fatalf("recovered rows=%+v err=%v", rows, err)
	}
}

func TestStoreQueryInLocationUsesAbsoluteBuckets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer-traffic.json")
	now := time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC)
	store := New(path, func() time.Time { return now })
	store.Record(RouteDirect, DirectionSent, 100)
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	rows, coverage, err := store.QueryInLocation("2026-11-05", "2026-11-05", shanghai)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Day != "2026-11-05" || rows[0].BytesSent != 100 {
		t.Fatalf("Shanghai rows=%+v", rows)
	}
	if coverage != now.Unix()/900*900 {
		t.Fatalf("coverage=%d", coverage)
	}
	losAngeles, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err = store.QueryInLocation("2026-11-04", "2026-11-04", losAngeles)
	if err != nil || len(rows) != 1 || rows[0].Day != "2026-11-04" {
		t.Fatalf("Los Angeles rows=%+v err=%v", rows, err)
	}
}

func TestStoreV1RowsRemainUTCOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer-traffic.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"rows":[{"day":"2026-11-05","route":"direct","bytes_sent":10,"bytes_received":0,"records_sent":1,"records_received":0}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(path, time.Now)
	utcRows, err := store.Query(time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC))
	if err != nil || len(utcRows) != 1 {
		t.Fatalf("UTC rows=%+v err=%v", utcRows, err)
	}
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	localRows, coverage, err := store.QueryInLocation("2026-11-05", "2026-11-05", zone)
	if err != nil || len(localRows) != 0 || coverage != 0 {
		t.Fatalf("local rows=%+v coverage=%d err=%v", localRows, coverage, err)
	}
}
