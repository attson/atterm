package relay

import "testing"

func TestWriteWaitFor_LoopbackGetsTheLongBound(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:51234", "[::1]:51234", "127.0.0.53:9"} {
		if got := writeWaitFor(addr); got != clientWriteWaitLoopback {
			t.Fatalf("writeWaitFor(%q) = %v, want the loopback bound", addr, got)
		}
	}
}

// A real peer can vanish, so the short bound is what detects it.
func TestWriteWaitFor_RemoteKeepsTheShortBound(t *testing.T) {
	for _, addr := range []string{"203.0.113.7:443", "[2001:db8::1]:443", "10.0.0.5:8080"} {
		if got := writeWaitFor(addr); got != clientWriteWait {
			t.Fatalf("writeWaitFor(%q) = %v, want the remote bound", addr, got)
		}
	}
}

// When the address cannot be read, assume remote: cutting a stalled peer early
// is recoverable, leaving a dead one attached for five minutes is not.
func TestWriteWaitFor_UnparseableIsTreatedAsRemote(t *testing.T) {
	for _, addr := range []string{"", "pipe", "not-an-address"} {
		if got := writeWaitFor(addr); got != clientWriteWait {
			t.Fatalf("writeWaitFor(%q) = %v, want the remote bound", addr, got)
		}
	}
}
