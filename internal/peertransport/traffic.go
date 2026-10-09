package peertransport

// TrafficDirection identifies one successfully authenticated encrypted
// application record relative to the local device.
type TrafficDirection uint8

const (
	TrafficSent TrafficDirection = iota + 1
	TrafficReceived
)

// TrafficObserver receives only direction and encrypted record size. Payloads,
// Peer identities, session identifiers and network endpoints are never exposed.
type TrafficObserver func(TrafficDirection, int)
