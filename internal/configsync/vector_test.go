package configsync

import (
	"reflect"
	"testing"
)

func TestVersionVectorRelationsAndRanges(t *testing.T) {
	a := VersionVector{"a": 2, "b": 1}
	if got := a.Compare(VersionVector{"a": 2, "b": 1}); got != VectorEqual {
		t.Fatalf("equal relation = %v", got)
	}
	if got := a.Compare(VersionVector{"a": 3, "b": 1}); got != VectorBefore {
		t.Fatalf("before relation = %v", got)
	}
	if got := a.Compare(VersionVector{"a": 1}); got != VectorAfter {
		t.Fatalf("after relation = %v", got)
	}
	if got := a.Compare(VersionVector{"a": 3}); got != VectorConcurrent {
		t.Fatalf("concurrent relation = %v", got)
	}

	wantRanges := map[string]CounterRange{
		"a": {From: 3, To: 5},
		"c": {From: 1, To: 4},
	}
	if got := a.MissingFrom(VersionVector{"a": 5, "b": 1, "c": 4}); !reflect.DeepEqual(got, wantRanges) {
		t.Fatalf("missing ranges = %#v, want %#v", got, wantRanges)
	}

	clone := a.Clone()
	clone.Merge(VersionVector{"a": 4, "c": 2})
	if !clone.Covers(a) || !reflect.DeepEqual(clone, VersionVector{"a": 4, "b": 1, "c": 2}) {
		t.Fatalf("merged vector = %#v", clone)
	}
}
