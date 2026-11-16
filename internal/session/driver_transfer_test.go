package session

import (
	"sync"
	"testing"
)

func TestTransferDriverIfCurrentPreservesIdentityWithoutPublishingMetadata(t *testing.T) {
	s := newSessionForTest(t)
	defer s.Close()
	old, _ := s.Subscribe(0, "proxy-a", "remote", WithoutAutoDrive())
	next, _ := s.Subscribe(0, "proxy-b", "remote", WithoutAutoDrive())
	local, _ := s.Subscribe(0, "local", "desktop", WithoutAutoDrive())
	s.ClaimDriver(old, "remote-user", "phone")
	for _, sub := range []*Subscriber{old, next, local} {
		drainOutputFrames(sub)
	}
	if !s.TransferDriverIfCurrent(old, next) {
		t.Fatal("current driver was not transferred")
	}
	if !s.IsDriver(next) || s.DriverClientID() != "remote-user" || s.DriverClientName() != "phone" {
		t.Fatal("driver identity changed with its proxy")
	}
	for _, sub := range []*Subscriber{old, next, local} {
		select {
		case <-sub.Out():
			t.Fatal("internal proxy transfer published redundant driver metadata")
		default:
		}
	}
	s.ClaimDriver(local, "local", "desktop")
	for _, sub := range []*Subscriber{old, next, local} {
		drainOutputFrames(sub)
	}
	if s.TransferDriverIfCurrent(old, next) || !s.IsDriver(local) || s.DriverClientID() != "local" {
		t.Fatal("stale output-lane transfer stole the local driver's claim")
	}
	select {
	case <-local.Out():
		t.Fatal("rejected transfer published stale driver metadata")
	default:
	}
	s.Unsubscribe(next)
	if s.TransferDriverIfCurrent(local, next) || !s.IsDriver(local) {
		t.Fatal("driver transferred to an unregistered subscriber")
	}
}

func TestConcurrentLocalClaimWinsOutputLaneDriverTransfer(t *testing.T) {
	s := newSessionForTest(t)
	defer s.Close()
	old, _ := s.Subscribe(0, "proxy-a", "remote", WithoutAutoDrive())
	next, _ := s.Subscribe(0, "proxy-b", "remote", WithoutAutoDrive())
	local, _ := s.Subscribe(0, "local", "desktop", WithoutAutoDrive())
	for i := 0; i < 64; i++ {
		s.ClaimDriver(old, "remote-user", "phone")
		start := make(chan struct{})
		var workers sync.WaitGroup
		workers.Add(2)
		go func() { defer workers.Done(); <-start; s.TransferDriverIfCurrent(old, next) }()
		go func() { defer workers.Done(); <-start; s.ClaimDriver(local, "local", "desktop") }()
		close(start)
		workers.Wait()
		if !s.IsDriver(local) || s.DriverClientID() != "local" || s.DriverClientName() != "desktop" {
			t.Fatal("output-lane shutdown stole a concurrent local driver's claim")
		}
	}
}
