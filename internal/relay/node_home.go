package relay

import (
	"context"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// InstanceLivenessWindow is how recent an instance's heartbeat must be to be
// considered live in node selection. Heartbeats are written every ~30s.
const InstanceLivenessWindow = 90 * time.Second

// resolveHomeInstanceURL computes the home_instance_url for a login on the node
// identified by thisInstanceID (its public URL; "" for single-instance/dev).
//   - thisInstanceID == "": no node selection → "".
//   - user_home set and that instance is live → its public URL.
//   - user_home set but instance is dead → "" (client re-selects; we don't relocate).
//   - user_home unset → auto-assign the serving node and return its URL.
func resolveHomeInstanceURL(ctx context.Context, store userstore.Store, userID, thisInstanceID string) (string, error) {
	if thisInstanceID == "" {
		return "", nil
	}
	minHB := time.Now().Add(-InstanceLivenessWindow).Unix()
	live, err := store.ListLiveInstances(ctx, minHB)
	if err != nil {
		return "", err
	}
	liveURL := make(map[string]string, len(live)+1)
	for _, inst := range live {
		liveURL[inst.InstanceID] = inst.PublicURL
	}
	// The node serving this login is reachable by definition, even if its
	// heartbeat row hasn't been written yet. instance_id == public_url.
	liveURL[thisInstanceID] = thisInstanceID

	homeID, ok, err := store.GetUserHome(ctx, userID)
	if err != nil {
		return "", err
	}
	if ok {
		if url, alive := liveURL[homeID]; alive {
			return url, nil
		}
		return "", nil // selected node is dead
	}
	if err := store.SetUserHome(ctx, userID, thisInstanceID); err != nil {
		return "", err
	}
	return thisInstanceID, nil
}
