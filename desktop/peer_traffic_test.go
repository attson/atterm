package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peertraffic"
	"github.com/attson/atterm/internal/peertransport"
)

func TestGetPeerTrafficFlushesAndReturnsBoundedLocalAggregates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer-traffic.json")
	now := time.Date(2026, 10, 9, 23, 30, 0, 0, time.FixedZone("east", 8*60*60))
	app := &App{peerTraffic: peertraffic.New(path, func() time.Time { return now })}
	direct := app.recordPeerTraffic(peertraffic.RouteDirect)
	direct(peertransport.TrafficSent, 123)
	direct(peertransport.TrafficReceived, 45)

	rows, err := app.GetPeerTraffic("2026-10-09", "2026-10-09")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%+v", rows)
	}
	if got := rows[0]; got.Day != "2026-10-09" || got.Route != "direct" ||
		got.BytesSent != 123 || got.BytesReceived != 45 || got.RecordsSent != 1 || got.RecordsReceived != 1 {
		t.Fatalf("row=%+v", got)
	}
	reopened := peertraffic.New(path, func() time.Time { return now })
	persisted, err := reopened.Query(now, now)
	if err != nil || len(persisted) != 1 || persisted[0].BytesSent != 123 {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
}

func TestGetPeerTrafficRejectsInvalidOrOversizedRanges(t *testing.T) {
	app := &App{peerTraffic: peertraffic.New(filepath.Join(t.TempDir(), "peer-traffic.json"), time.Now)}
	for _, test := range []struct {
		from string
		to   string
	}{
		{from: "2026/10/09", to: "2026-10-09"},
		{from: "2026-10-10", to: "2026-10-09"},
		{from: "2026-01-01", to: "2026-04-01"},
	} {
		if _, err := app.GetPeerTraffic(test.from, test.to); err == nil {
			t.Fatalf("range %q..%q accepted", test.from, test.to)
		}
	}
	if _, err := (&App{}).GetPeerTraffic("2026-10-09", "2026-10-09"); err == nil {
		t.Fatal("missing meter accepted")
	}
}

func TestShutdownFlushesPeerTraffic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer-traffic.json")
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	store := peertraffic.New(path, func() time.Time { return now })
	app := &App{peerTraffic: store}
	app.recordPeerTraffic(peertraffic.RouteLAN)(peertransport.TrafficSent, 77)
	app.shutdown(context.Background())

	rows, err := peertraffic.New(path, func() time.Time { return now }).Query(now, now)
	if err != nil || len(rows) != 1 || rows[0].Route != peertraffic.RouteLAN || rows[0].BytesSent != 77 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}
