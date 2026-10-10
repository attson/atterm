package peertransport

import "github.com/google/uuid"

var configSyncSessionID = uuid.MustParse("ffffffff-ffff-4fff-bfff-ffffffffffff")

// ConfigSyncSessionID is a reserved transcript namespace for authenticated
// Peer control routes. It never identifies a terminal session or subscriber.
func ConfigSyncSessionID() uuid.UUID { return configSyncSessionID }
