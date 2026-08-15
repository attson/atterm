package relay

import (
	"net"
	"time"
)

// Both are vars, not consts: tests shorten them to keep the suite fast.
var (
	// clientWriteWait bounds a single write to a client before the connection
	// is torn down. It exists to detect a peer that has stopped reading and
	// will never resume — a closed laptop lid, a dropped network.
	clientWriteWait = 10 * time.Second

	// clientWriteWaitLoopback is the same bound for a client on this machine.
	//
	// The desktop frontend is one: it reaches its own in-process relay over
	// 127.0.0.1, so every local terminal is a websocket client. There is no
	// partition to detect on loopback — a socket that stops draining there
	// means the renderer is busy, typically parsing a burst of output, and it
	// will come back. Tearing it down instead makes that worse: the client
	// reconnects, replays, and falls behind again, which the user sees as a
	// local terminal that keeps saying "reconnecting". Liveness is still
	// covered by the ping loop, which fails far sooner than this.
	clientWriteWaitLoopback = 5 * time.Minute
)

// writeWaitFor picks the write bound for a client at addr.
func writeWaitFor(remoteAddr string) time.Duration {
	if isLoopbackAddr(remoteAddr) {
		return clientWriteWaitLoopback
	}
	return clientWriteWait
}

// isLoopbackAddr reports whether a "host:port" belongs to this machine.
// An unparseable address is treated as remote: the stricter bound is the safe
// default when we cannot tell.
func isLoopbackAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
