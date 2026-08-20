package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/prefssync"
)

// The Vue side keys the "changed on another device" notice off these exact
// field names. Go's struct tags decide them, so a tag change silently breaks
// the notice with no compile error anywhere -- the TS interface is a
// hand-written mirror, not a generated one.
func TestPullResultJSONFieldNamesMatchTheShim(t *testing.T) {
	b, err := json.Marshal(prefssync.PullResult{Adopted: []string{"a"}, Conflict: []string{"c"}})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"adopted"`, `"conflict"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("PullResult JSON = %s, want it to contain %s (desktop/frontend/src/lib/api/_bindings.ts mirrors these names by hand)", got, want)
		}
	}
}
