package relay

import "time"

// InstanceLivenessWindow is how recent an instance's heartbeat must be to be
// considered live in node selection. Heartbeats are written every ~30s.
const InstanceLivenessWindow = 90 * time.Second
