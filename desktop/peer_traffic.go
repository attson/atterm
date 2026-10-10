package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/peertraffic"
	"github.com/attson/atterm/internal/peertransport"
)

const maxPeerTrafficQueryDays = 90

// PeerTrafficRow is a device-local aggregate. It deliberately has no Peer,
// session or endpoint dimensions.
type PeerTrafficRow struct {
	Day             string `json:"day"`
	Route           string `json:"route"`
	BytesSent       uint64 `json:"bytes_sent"`
	BytesReceived   uint64 `json:"bytes_received"`
	RecordsSent     uint64 `json:"records_sent"`
	RecordsReceived uint64 `json:"records_received"`
}

func newPeerTrafficStore() (*peertraffic.Store, error) {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("Peer traffic config directory: %w", err)
	}
	return peertraffic.New(filepath.Join(dir, "peer-traffic.json"), time.Now), nil
}

func (a *App) recordPeerTraffic(route peertraffic.Route) peertransport.TrafficObserver {
	return func(direction peertransport.TrafficDirection, wireBytes int) {
		if a == nil || a.peerTraffic == nil {
			return
		}
		storeDirection := peertraffic.DirectionSent
		if direction == peertransport.TrafficReceived {
			storeDirection = peertraffic.DirectionReceived
		} else if direction != peertransport.TrafficSent {
			return
		}
		a.peerTraffic.Record(route, storeDirection, wireBytes)
	}
}

// GetPeerTraffic returns an inclusive UTC range of privacy-preserving local
// encrypted-record totals. Renderer requests are bounded to 90 days.
func (a *App) GetPeerTraffic(from, to string) ([]PeerTrafficRow, error) {
	if a == nil || a.peerTraffic == nil {
		return nil, errors.New("Peer traffic meter unavailable")
	}
	fromTime, err := time.Parse(time.DateOnly, from)
	if err != nil || fromTime.Format(time.DateOnly) != from {
		return nil, errors.New("invalid Peer traffic start date")
	}
	toTime, err := time.Parse(time.DateOnly, to)
	if err != nil || toTime.Format(time.DateOnly) != to || toTime.Before(fromTime) {
		return nil, errors.New("invalid Peer traffic end date")
	}
	if toTime.Sub(fromTime) > (maxPeerTrafficQueryDays-1)*24*time.Hour {
		return nil, errors.New("Peer traffic range exceeds 90 days")
	}
	rows, err := a.peerTraffic.Query(fromTime, toTime)
	if err != nil {
		return nil, fmt.Errorf("query Peer traffic: %w", err)
	}
	result := make([]PeerTrafficRow, len(rows))
	for index, row := range rows {
		result[index] = PeerTrafficRow{
			Day: row.Day, Route: string(row.Route), BytesSent: row.BytesSent,
			BytesReceived: row.BytesReceived, RecordsSent: row.RecordsSent,
			RecordsReceived: row.RecordsReceived,
		}
	}
	return result, nil
}
