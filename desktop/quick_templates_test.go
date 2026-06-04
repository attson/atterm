package main

import (
	"reflect"
	"testing"
)

func TestGetQuickTemplates_FreshAppReturnsEmpty(t *testing.T) {
	a := newRelayTestApp(t)
	got := a.GetQuickTemplates()
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(got))
	}
}

func TestSetQuickTemplates_RoundTrips(t *testing.T) {
	a := newRelayTestApp(t)
	want := []QuickTemplate{
		{ID: "a", Label: "A", Text: "a-text"},
		{ID: "b", Label: "B", Text: "b-text"},
	}
	if err := a.SetQuickTemplates(want); err != nil {
		t.Fatalf("SetQuickTemplates: %v", err)
	}
	got := a.GetQuickTemplates()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch:\n got=%v\nwant=%v", got, want)
	}
}

func TestSetQuickTemplates_EmptyClears(t *testing.T) {
	a := newRelayTestApp(t)
	_ = a.SetQuickTemplates([]QuickTemplate{{ID: "x", Label: "x", Text: "x"}})
	if err := a.SetQuickTemplates(nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got := a.GetQuickTemplates()
	if len(got) != 0 {
		t.Fatalf("expected empty after clear, got %d", len(got))
	}
}
