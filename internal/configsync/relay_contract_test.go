package configsync_test

import (
	"reflect"
	"testing"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/prefssync"
)

func TestCanonicalActiveSchemaMatchesDesktopSync(t *testing.T) {
	if got, want := configsync.ActiveRelayKeys(), prefssync.SyncedKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("configsync Relay keys=%v; prefssync keys=%v", got, want)
	}
	spec, ok := configsync.RelaySpec("profiles_encrypted")
	if !ok || !spec.Retired {
		t.Fatalf("profiles tombstone=%+v ok=%t", spec, ok)
	}
	for _, spec := range configsync.RelayKeySpecs() {
		if spec.Retired != (spec.Key == "profiles_encrypted") {
			t.Fatalf("unexpected retirement: %+v", spec)
		}
	}
	tz, ok := configsync.RelaySpec("traffic_time_zone")
	if !ok || tz.Retired || tz.Mode != configsync.RelayScalar {
		t.Fatalf("timezone spec=%+v ok=%t", tz, ok)
	}
}
