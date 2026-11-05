// Package peertraffic persists privacy-preserving, device-local Peer route
// usage. Rows contain only absolute 15-minute time, route class and aggregate encrypted record
// counts; peer identities, session IDs and network endpoints are never stored.
package peertraffic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	storeVersion  = 2
	maxStoreSize  = 16 << 20
	retentionDays = 180
	lockTimeout   = 2 * time.Second
	staleLockAge  = 30 * time.Second
)

// Route identifies a privacy-preserving transport class.
type Route string

const (
	RouteDirect      Route = "direct"
	RouteQuickTunnel Route = "quick_tunnel"
	RouteLAN         Route = "lan"
	// RouteGateway is used by a shared host-side WSS gateway where the same
	// authenticated handler can be reached through Quick Tunnel or manual LAN.
	RouteGateway Route = "gateway"
)

// Direction is relative to the local desktop process.
type Direction uint8

const (
	DirectionSent Direction = iota + 1
	DirectionReceived
)

// Row is one local UTC-day and route aggregate suitable for renderer output.
type Row struct {
	Day             string `json:"day"`
	Route           Route  `json:"route"`
	BytesSent       uint64 `json:"bytes_sent"`
	BytesReceived   uint64 `json:"bytes_received"`
	RecordsSent     uint64 `json:"records_sent"`
	RecordsReceived uint64 `json:"records_received"`
}

type state struct {
	Version      int         `json:"version"`
	Rows         []Row       `json:"rows,omitempty"`
	Buckets      []bucketRow `json:"buckets,omitempty"`
	CoverageFrom int64       `json:"coverage_from,omitempty"`
}

type bucketRow struct {
	StartedAt       int64  `json:"started_at"`
	Route           Route  `json:"route"`
	BytesSent       uint64 `json:"bytes_sent"`
	BytesReceived   uint64 `json:"bytes_received"`
	RecordsSent     uint64 `json:"records_sent"`
	RecordsReceived uint64 `json:"records_received"`
}

type key struct {
	startedAt int64
	route     Route
}

// Store keeps hot-path increments in memory and merges them into one bounded,
// cross-process-safe JSON file on Flush or Query.
type Store struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	pending map[key]bucketRow
}

// New constructs a local aggregate store. The file is created on first flush.
func New(path string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{path: path, now: now, pending: make(map[key]bucketRow)}
}

// Record adds one successfully authenticated encrypted record. It performs no
// I/O and deliberately ignores invalid route/direction/size input.
func (s *Store) Record(route Route, direction Direction, wireBytes int) {
	if s == nil || !validRoute(route) || wireBytes <= 0 ||
		direction != DirectionSent && direction != DirectionReceived {
		return
	}
	startedAt := s.now().UTC().Unix() / 900 * 900
	k := key{startedAt: startedAt, route: route}
	s.mu.Lock()
	row := s.pending[k]
	row.StartedAt = startedAt
	row.Route = route
	if direction == DirectionSent {
		row.BytesSent += uint64(wireBytes)
		row.RecordsSent++
	} else {
		row.BytesReceived += uint64(wireBytes)
		row.RecordsReceived++
	}
	s.pending[k] = row
	s.mu.Unlock()
}

// Flush durably merges this process's pending deltas with other desktop
// instances. Failed deltas are returned to memory for a later retry.
func (s *Store) Flush() error {
	if s == nil || s.path == "" {
		return errors.New("peertraffic: store path is missing")
	}
	pending := s.takePending()
	if len(pending) == 0 {
		return nil
	}
	err := s.withFileLock(func() error {
		persisted, err := s.load()
		if err != nil {
			return err
		}
		rows := make(map[key]bucketRow, len(persisted.Buckets)+len(pending))
		for _, row := range persisted.Buckets {
			rows[key{startedAt: row.StartedAt, route: row.Route}] = row
		}
		for k, delta := range pending {
			row := rows[k]
			row.StartedAt, row.Route = k.startedAt, k.route
			row.BytesSent += delta.BytesSent
			row.BytesReceived += delta.BytesReceived
			row.RecordsSent += delta.RecordsSent
			row.RecordsReceived += delta.RecordsReceived
			rows[k] = row
		}
		cutoff := s.now().UTC().AddDate(0, 0, -retentionDays+1).Unix()
		next := state{Version: storeVersion, Rows: persisted.Rows, Buckets: make([]bucketRow, 0, len(rows)), CoverageFrom: persisted.CoverageFrom}
		if next.CoverageFrom == 0 {
			for k := range pending {
				if next.CoverageFrom == 0 || k.startedAt < next.CoverageFrom {
					next.CoverageFrom = k.startedAt
				}
			}
		}
		for _, row := range rows {
			if row.StartedAt >= cutoff {
				next.Buckets = append(next.Buckets, row)
			}
		}
		sortBuckets(next.Buckets)
		return s.write(next)
	})
	if err != nil {
		s.restorePending(pending)
	}
	return err
}

// Query flushes current deltas and returns inclusive UTC-day rows.
func (s *Store) Query(from, to time.Time) ([]Row, error) {
	if s == nil || from.IsZero() || to.IsZero() {
		return nil, errors.New("peertraffic: invalid range")
	}
	fromDay := from.UTC().Format(time.DateOnly)
	toDay := to.UTC().Format(time.DateOnly)
	if fromDay > toDay {
		return nil, errors.New("peertraffic: invalid range")
	}
	if err := s.Flush(); err != nil {
		return nil, err
	}
	var persisted state
	if err := s.withFileLock(func() error {
		var err error
		persisted, err = s.load()
		return err
	}); err != nil {
		return nil, err
	}
	aggregated := make(map[string]Row)
	for _, row := range persisted.Rows {
		if row.Day >= fromDay && row.Day <= toDay {
			aggregated[row.Day+"\x00"+string(row.Route)] = row
		}
	}
	for _, bucket := range persisted.Buckets {
		day := time.Unix(bucket.StartedAt, 0).UTC().Format(time.DateOnly)
		if day < fromDay || day > toDay {
			continue
		}
		key := day + "\x00" + string(bucket.Route)
		row := aggregated[key]
		row.Day, row.Route = day, bucket.Route
		row.BytesSent += bucket.BytesSent
		row.BytesReceived += bucket.BytesReceived
		row.RecordsSent += bucket.RecordsSent
		row.RecordsReceived += bucket.RecordsReceived
		aggregated[key] = row
	}
	rows := make([]Row, 0, len(aggregated))
	for _, row := range aggregated {
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}

// QueryInLocation returns reporting-day rows from absolute buckets. Legacy v1
// UTC rows are intentionally excluded because their original instants are lost.
func (s *Store) QueryInLocation(fromDay, toDay string, zone *time.Location) ([]Row, int64, error) {
	if s == nil || zone == nil || !validDay(fromDay) || !validDay(toDay) || fromDay > toDay {
		return nil, 0, errors.New("peertraffic: invalid reporting range")
	}
	if err := s.Flush(); err != nil {
		return nil, 0, err
	}
	var persisted state
	if err := s.withFileLock(func() error { var err error; persisted, err = s.load(); return err }); err != nil {
		return nil, 0, err
	}
	aggregated := make(map[string]Row)
	for _, bucket := range persisted.Buckets {
		day := time.Unix(bucket.StartedAt, 0).In(zone).Format(time.DateOnly)
		if day < fromDay || day > toDay {
			continue
		}
		key := day + "\x00" + string(bucket.Route)
		row := aggregated[key]
		row.Day, row.Route = day, bucket.Route
		row.BytesSent += bucket.BytesSent
		row.BytesReceived += bucket.BytesReceived
		row.RecordsSent += bucket.RecordsSent
		row.RecordsReceived += bucket.RecordsReceived
		aggregated[key] = row
	}
	rows := make([]Row, 0, len(aggregated))
	for _, row := range aggregated {
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, persisted.CoverageFrom, nil
}

func (s *Store) takePending() map[key]bucketRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending
	s.pending = make(map[key]bucketRow)
	return pending
}

func (s *Store) restorePending(failed map[key]bucketRow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, delta := range failed {
		row := s.pending[k]
		row.StartedAt, row.Route = k.startedAt, k.route
		row.BytesSent += delta.BytesSent
		row.BytesReceived += delta.BytesReceived
		row.RecordsSent += delta.RecordsSent
		row.RecordsReceived += delta.RecordsReceived
		s.pending[k] = row
	}
}

func (s *Store) load() (state, error) {
	blob, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return state{Version: storeVersion, Rows: []Row{}, Buckets: []bucketRow{}}, nil
	}
	if err != nil {
		return state{}, fmt.Errorf("peertraffic: read store: %w", err)
	}
	if len(blob) == 0 || len(blob) > maxStoreSize {
		return state{}, errors.New("peertraffic: invalid store size")
	}
	var persisted state
	if err := strictJSON(blob, &persisted); err != nil {
		return state{}, fmt.Errorf("peertraffic: decode store: %w", err)
	}
	if persisted.Version != 1 && persisted.Version != storeVersion {
		return state{}, errors.New("peertraffic: unsupported store version")
	}
	legacySeen := make(map[string]struct{}, len(persisted.Rows))
	for _, row := range persisted.Rows {
		k := row.Day + "\x00" + string(row.Route)
		if !validDay(row.Day) || !validRoute(row.Route) {
			return state{}, errors.New("peertraffic: invalid row")
		}
		if _, duplicate := legacySeen[k]; duplicate {
			return state{}, errors.New("peertraffic: duplicate row")
		}
		legacySeen[k] = struct{}{}
	}
	seen := make(map[key]struct{}, len(persisted.Buckets))
	for _, row := range persisted.Buckets {
		k := key{startedAt: row.StartedAt, route: row.Route}
		if row.StartedAt <= 0 || row.StartedAt%900 != 0 || !validRoute(row.Route) {
			return state{}, errors.New("peertraffic: invalid bucket")
		}
		if _, duplicate := seen[k]; duplicate {
			return state{}, errors.New("peertraffic: duplicate bucket")
		}
		seen[k] = struct{}{}
	}
	persisted.Version = storeVersion
	return persisted, nil
}

func (s *Store) write(next state) error {
	blob, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("peertraffic: encode store: %w", err)
	}
	if len(blob) > maxStoreSize {
		return errors.New("peertraffic: store size limit exceeded")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("peertraffic: create directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".peer-traffic-*.tmp")
	if err != nil {
		return fmt.Errorf("peertraffic: create temp: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(blob); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("peertraffic: replace store: %w", err)
	}
	return nil
}

func (s *Store) withFileLock(run func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	lockPath := s.path + ".lock"
	deadline := time.Now().Add(lockTimeout)
	for {
		err := os.Mkdir(lockPath, 0o700)
		if err == nil {
			defer os.Remove(lockPath)
			return run()
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("peertraffic: acquire lock: %w", err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > staleLockAge {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return errors.New("peertraffic: timed out waiting for lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func validRoute(route Route) bool {
	return route == RouteDirect || route == RouteQuickTunnel || route == RouteLAN || route == RouteGateway
}

func validDay(day string) bool {
	parsed, err := time.Parse(time.DateOnly, day)
	return err == nil && parsed.Format(time.DateOnly) == day
}

func sortRows(rows []Row) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Day != rows[j].Day {
			return rows[i].Day < rows[j].Day
		}
		return rows[i].Route < rows[j].Route
	})
}

func sortBuckets(rows []bucketRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].StartedAt != rows[j].StartedAt {
			return rows[i].StartedAt < rows[j].StartedAt
		}
		return rows[i].Route < rows[j].Route
	})
}

func strictJSON(blob []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(blob))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return err
	}
	return nil
}
