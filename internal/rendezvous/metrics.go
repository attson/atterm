package rendezvous

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

type metrics struct {
	activeConnections atomic.Int64
	registeredPeers   atomic.Int64
	mailboxMessages   atomic.Int64
	acceptedTotal     atomic.Uint64
	rejectedTotal     atomic.Uint64
	forwardedTotal    atomic.Uint64
	queuedTotal       atomic.Uint64
	expiredTotal      atomic.Uint64
}

func (m *metrics) serveHTTP(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, "# TYPE atterm_rendezvous_active_connections gauge\natterm_rendezvous_active_connections %d\n", m.activeConnections.Load())
	_, _ = fmt.Fprintf(w, "# TYPE atterm_rendezvous_registered_peers gauge\natterm_rendezvous_registered_peers %d\n", m.registeredPeers.Load())
	_, _ = fmt.Fprintf(w, "# TYPE atterm_rendezvous_mailbox_messages gauge\natterm_rendezvous_mailbox_messages %d\n", m.mailboxMessages.Load())
	_, _ = fmt.Fprintf(w, "# TYPE atterm_rendezvous_accepted_connections_total counter\natterm_rendezvous_accepted_connections_total %d\n", m.acceptedTotal.Load())
	_, _ = fmt.Fprintf(w, "# TYPE atterm_rendezvous_rejected_connections_total counter\natterm_rendezvous_rejected_connections_total %d\n", m.rejectedTotal.Load())
	_, _ = fmt.Fprintf(w, "# TYPE atterm_rendezvous_forwarded_messages_total counter\natterm_rendezvous_forwarded_messages_total %d\n", m.forwardedTotal.Load())
	_, _ = fmt.Fprintf(w, "# TYPE atterm_rendezvous_queued_messages_total counter\natterm_rendezvous_queued_messages_total %d\n", m.queuedTotal.Load())
	_, _ = fmt.Fprintf(w, "# TYPE atterm_rendezvous_expired_messages_total counter\natterm_rendezvous_expired_messages_total %d\n", m.expiredTotal.Load())
}
