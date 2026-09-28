package configsync_test

import (
	"reflect"
	"testing"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/prefssync"
)

func TestCanonicalSchemaCoversEveryLegacyRelayKey(t *testing.T) {
	if got, want := configsync.RelayKeys(), prefssync.SyncedKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("configsync Relay keys=%v; prefssync keys=%v", got, want)
	}
}
