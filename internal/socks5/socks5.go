// Package socks5 is a minimal SOCKS5 server (RFC 1928) whose CONNECTs are
// handed to an injected dialer.
//
// It exists because Go has no SOCKS5 *server*: golang.org/x/net/proxy is a
// client only. atterm needs one for dynamic port forwarding (`ssh -D`), where
// every CONNECT becomes a direct-tcpip channel on an SSH connection.
//
// The dialer is injected rather than imported for a reason that is about
// testing, not about layering: hand-written protocol parsing fails on
// malformed input, and this package can be driven to every one of those
// failures with net.Pipe and no SSH, no listener on the far side and no route.
// Nothing here may import the SSH client or anything from desktop/.
//
// # Scope
//
// Deliberately partial, per the design's §5.4:
//
//   - Authentication: NO AUTHENTICATION (X'00') only. Anything else gets
//     X'FF'.
//   - Commands: CONNECT (X'01') only. BIND and UDP ASSOCIATE are answered with
//     X'07' (command not supported), never with a silent hangup — a client
//     that just loses the connection cannot tell "unsupported" from "broken".
//   - Address types: IPv4, DOMAINNAME, IPv6. Names are passed through
//     unresolved, so they resolve on the far side of the tunnel, which is the
//     whole point of using a SOCKS proxy over SSH.
//
// UDP ASSOCIATE is not implemented because SSH port forwarding does not carry
// UDP at all; BIND is not implemented because essentially nothing uses it.
package socks5

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"time"
)

// Protocol constants (RFC 1928).
const (
	version = 0x05

	methodNoAuth       = 0x00
	methodNoAcceptable = 0xFF

	cmdConnect      = 0x01
	cmdBind         = 0x02
	cmdUDPAssociate = 0x03

	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04

	repSuccess         = 0x00
	repGeneralFailure  = 0x01
	repHostUnreachable = 0x04
	// repConnectionRefused is emitted only when the dialer says so, by
	// wrapping ErrDestinationRefused. Guessing it from any other failure would
	// send the user to the wrong machine — see replyCodeForDialError.
	repConnectionRefused    = 0x05
	repCommandNotSupported  = 0x07
	repAddrTypeNotSupported = 0x08
)

// defaultHandshakeTimeout bounds the whole method-selection + request
// exchange. A client that connects and then says nothing would otherwise hold
// a goroutine and a socket forever, and opening a few thousand such
// connections costs the client nothing.
//
// The deadline is *cleared* once a CONNECT succeeds: a proxied connection may
// legitimately idle for hours (an ssh session, a database connection), and a
// handshake deadline left in place would cut it at exactly this interval.
const defaultHandshakeTimeout = 30 * time.Second

// handshakeTimeoutNanos lets tests shrink the timeout. It is atomic rather
// than a plain var because a test that shortens it does so while connections
// from earlier tests are still being served in the background — a plain var
// is a data race there, and the race detector is right about it.
// Zero (the zero value) means defaultHandshakeTimeout.
var handshakeTimeoutNanos atomic.Int64

func handshakeDeadline() time.Time {
	d := time.Duration(handshakeTimeoutNanos.Load())
	if d <= 0 {
		d = defaultHandshakeTimeout
	}
	return time.Now().Add(d)
}

// Dialer opens the far side of a CONNECT. addr is "host:port" with host either
// a literal IP or a name that has deliberately *not* been resolved here.
//
// A Dialer that can distinguish "the destination refused this connection" from
// "the path to it is broken" should wrap ErrDestinationRefused around the
// former; see replyCodeForDialError for what that buys the client.
type Dialer func(network, addr string) (net.Conn, error)

// ErrDestinationRefused marks a dial failure that the far side attributed to
// the *destination itself* — something answered for that host and port, and
// said no.
//
// It exists because this package cannot tell that apart from a dead transport
// on its own, and the caller can. The SSH dialer's failure is an opaque error
// carrying a human-readable string, so classifying it here would mean either
// matching on that text or importing the SSH client — and importing the SSH
// client is exactly the dependency that keeps this package testable with
// net.Pipe and no network. A sentinel the caller wraps moves the one decision
// it can actually make across the boundary without moving the dependency.
//
// Wrap, do not replace: fmt.Errorf("%w: %w", socks5.ErrDestinationRefused, err)
// keeps the underlying error's text for the log.
var ErrDestinationRefused = errors.New("socks5: destination refused the connection")

// Serve accepts connections on l and serves each one as a SOCKS5 session,
// returning when Accept fails.
//
// It does not close l: the caller owns the listener, and closing it is how the
// caller stops the loop.
//
// atterm's tunnel manager does *not* use this — it runs its own accept loop
// over ServeConn, because it has to register each accepted connection before
// serving it so that stopping a rule cuts sessions that are already proxying.
// Serve is the plain form for callers with no such bookkeeping.
func Serve(l net.Listener, dial Dialer) error {
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer c.Close()
			_ = ServeConn(c, dial)
		}()
	}
}

// ServeConn runs one SOCKS5 session on an already-accepted connection and
// returns when it is finished — after the proxied connection has closed, or as
// soon as the exchange fails. It closes the connection it dialed, but leaves
// c to the caller.
//
// A returned error is a protocol-level fact about this one connection; it is
// never a reason to stop serving others.
func ServeConn(c net.Conn, dial Dialer) error {
	// One deadline over the entire handshake: every read and write of method
	// selection and the request inherits it, so no *protocol* step can block
	// forever.
	//
	// The dial in the middle is the exception, and it is worth being precise
	// about rather than implying otherwise: this package puts no timeout on the
	// injected Dialer, so a Dialer that can hang without ever failing hangs
	// here.
	//
	// atterm's own dialer does not bound it either, and that is a decision
	// rather than an oversight — see serveDynamicConn in desktop/ssh_tunnels.go.
	// It is an SSH channel open, bounded only indirectly: the tunnel's
	// keepalive closes the connection when the transport is dead and the
	// pending open fails with it, within one keepalive interval. A timeout
	// wrapped around it would produce an error the caller's own failure
	// classification reads as "transport dead", which would stop every tunnel
	// on that connection because one destination was slow. Any other caller
	// that cannot make that argument should bound its Dialer.
	_ = c.SetDeadline(handshakeDeadline())

	err := serveConn(c, dial)
	if err != nil {
		drainBeforeClose(c)
	}
	return err
}

func serveConn(c net.Conn, dial Dialer) error {
	if err := negotiateMethod(c); err != nil {
		return err
	}
	return handleRequest(c, dial)
}

// drainTimeout bounds the linger below. RFC 1928 asks a server to terminate a
// connection it has refused "within 10 seconds"; a quarter of a second is
// plenty to swallow a pipelined request and keeps a failed session from
// occupying anything for meaningfully long.
const drainTimeout = 250 * time.Millisecond

// drainBeforeClose reads and discards whatever the client has already sent, so
// that the close which follows is a FIN rather than an RST.
//
// This is not tidiness. Closing a TCP socket that still has unread data in its
// receive queue sends RST on Linux, and an RST makes the peer's kernel discard
// data it has already buffered — including the failure reply written a
// microsecond earlier. A client that pipelines payload straight after its
// CONNECT (perfectly legal, and common) would then see exactly the silent
// disconnect that X'07' and the dial-failure codes exist to avoid.
func drainBeforeClose(c net.Conn) {
	_ = c.SetReadDeadline(time.Now().Add(drainTimeout))
	_, _ = io.Copy(io.Discard, c)
}

// negotiateMethod runs RFC 1928 §3: the client offers methods, we pick one.
//
// We only ever pick NO AUTHENTICATION, and that is safe *only because of where
// the listener is bound*. atterm binds tunnel listeners to 127.0.0.1 by
// default (design §5.1), so the client has to already be on this machine. On a
// non-loopback bind this becomes an open proxy that anyone on the network can
// relay traffic through — with the SSH credential supplied by us, not by them.
//
// So: if you are here to add username/password auth, the reason to do it is
// not "defence in depth", it is that someone widened the bind address. Fix
// that first. And do not add an option that widens the bind for convenience.
func negotiateMethod(c net.Conn) error {
	var head [2]byte
	if _, err := io.ReadFull(c, head[:]); err != nil {
		return fmt.Errorf("socks5: reading greeting: %w", err)
	}
	if head[0] != version {
		// Not a SOCKS5 client at all (SOCKS4 starts with 0x04, and so does
		// every unlucky binary protocol that happens to open with that byte).
		// There is no reply that such a client could parse, so drop it rather
		// than send a v5 frame into a stream that will misread it.
		return fmt.Errorf("socks5: unsupported protocol version %#x", head[0])
	}

	n := int(head[1])
	methods := make([]byte, n) // n <= 255; ReadFull of an empty slice is a no-op
	if _, err := io.ReadFull(c, methods); err != nil {
		return fmt.Errorf("socks5: reading auth methods: %w", err)
	}

	// NMETHODS = 0 lands here with an empty list and therefore no acceptable
	// method — answered, not dropped.
	for _, m := range methods {
		if m == methodNoAuth {
			_, err := c.Write([]byte{version, methodNoAuth})
			return err
		}
	}
	if _, err := c.Write([]byte{version, methodNoAcceptable}); err != nil {
		return err
	}
	return errors.New("socks5: client offered no acceptable auth method")
}

// handleRequest runs RFC 1928 §4: read one request, answer it, and — for a
// CONNECT we accepted — splice until one side goes away.
func handleRequest(c net.Conn, dial Dialer) error {
	var head [4]byte // VER CMD RSV ATYP
	if _, err := io.ReadFull(c, head[:]); err != nil {
		return fmt.Errorf("socks5: reading request: %w", err)
	}
	if head[0] != version {
		// Same reasoning as the greeting: a peer that is not speaking v5 by
		// now cannot be told anything useful in a v5 frame.
		return fmt.Errorf("socks5: unsupported request version %#x", head[0])
	}

	cmd := head[1]
	// The address is read even for commands we refuse, so that the refusal
	// happens at a *frame boundary*. Replying early would leave the address
	// bytes unread in the stream — harmless once we hang up, but it means the
	// server's idea of where the request ends is wrong, which is how the next
	// refactor grows a desync bug.
	addr, err := readAddress(c, head[3])
	if err != nil {
		if errors.Is(err, errAddrTypeNotSupported) {
			// The address length is unknown for an unknown ATYP, so the stream
			// cannot be resynchronised — answer and hang up.
			_ = writeReply(c, repAddrTypeNotSupported)
			return err
		}
		return err
	}

	if cmd != cmdConnect {
		// BIND and UDP ASSOCIATE (and anything undefined) get the reply RFC
		// 1928 provides for exactly this, so the client can report "this proxy
		// does not do that" instead of "the connection broke".
		_ = writeReply(c, repCommandNotSupported)
		return unsupportedCommandError(cmd)
	}
	if addr.host == "" {
		// A zero-length DOMAINNAME frames correctly but is not an address.
		// Dialing ":443" would reach for something on the far side's loopback.
		_ = writeReply(c, repGeneralFailure)
		return errors.New("socks5: empty destination address")
	}

	target := net.JoinHostPort(addr.host, strconv.Itoa(int(addr.port)))
	remote, err := dial("tcp", target)
	// The dial can burn the whole handshake budget — connecting to a dead host
	// through an SSH channel is exactly the slow case. The reply still has to
	// get out: a client whose CONNECT took a while must not be answered with a
	// write that fails on an expired deadline, which reaches it as the silent
	// close every reply code here exists to avoid.
	_ = c.SetDeadline(handshakeDeadline())
	if err != nil {
		_ = writeReply(c, replyCodeForDialError(err))
		return fmt.Errorf("socks5: dial %s: %w", target, err)
	}
	if remote == nil {
		// A dialer that returns (nil, nil) is a bug in the caller, but it must
		// not become a nil dereference in a goroutine that takes the app down.
		_ = writeReply(c, repGeneralFailure)
		return fmt.Errorf("socks5: dialer returned no connection for %s", target)
	}

	if err := writeReply(c, repSuccess); err != nil {
		_ = remote.Close()
		return err
	}

	// The handshake is over; from here the connection is the user's and may
	// idle for as long as it likes.
	_ = c.SetDeadline(time.Time{})
	splice(c, remote)
	return nil
}

func unsupportedCommandError(cmd byte) error {
	switch cmd {
	case cmdBind:
		return errors.New("socks5: BIND is not supported")
	case cmdUDPAssociate:
		return errors.New("socks5: UDP ASSOCIATE is not supported (ssh port forwarding does not carry udp)")
	default:
		return fmt.Errorf("socks5: unknown command %#x", cmd)
	}
}

// address is a parsed destination. host is kept as a string precisely so a
// DOMAINNAME stays unresolved.
type address struct {
	host string
	port uint16
}

var errAddrTypeNotSupported = errors.New("socks5: unsupported address type")

// readAddress reads ATYP's address and the two port bytes. Every length here
// comes from the wire, so every read is a ReadFull against a bounded buffer:
// a length prefix that claims more than arrives ends as io.ErrUnexpectedEOF or
// as the handshake deadline, never as a wait with no end.
func readAddress(c net.Conn, atyp byte) (address, error) {
	var raw []byte
	switch atyp {
	case atypIPv4:
		raw = make([]byte, net.IPv4len)
	case atypIPv6:
		raw = make([]byte, net.IPv6len)
	case atypDomain:
		var n [1]byte
		if _, err := io.ReadFull(c, n[:]); err != nil {
			return address{}, fmt.Errorf("socks5: reading domain length: %w", err)
		}
		// The length prefix is a single byte, so the largest allocation a
		// client can provoke here is 255 bytes. The framing bounds it; there is
		// nothing further to clamp.
		raw = make([]byte, int(n[0]))
	default:
		return address{}, fmt.Errorf("%w %#x", errAddrTypeNotSupported, atyp)
	}
	if _, err := io.ReadFull(c, raw); err != nil {
		return address{}, fmt.Errorf("socks5: reading destination address: %w", err)
	}

	var port [2]byte
	if _, err := io.ReadFull(c, port[:]); err != nil {
		return address{}, fmt.Errorf("socks5: reading destination port: %w", err)
	}

	a := address{port: binary.BigEndian.Uint16(port[:])}
	if atyp == atypDomain {
		a.host = string(raw)
	} else {
		a.host = net.IP(raw).String()
	}
	return a, nil
}

// writeReply sends one reply frame.
//
// BND.ADDR/BND.PORT are reported as 0.0.0.0:0. For a CONNECT the fields are
// meant to be the address the server bound on the client's behalf, but here
// that address lives on the far side of an SSH channel and means nothing to
// the client; every mainstream SOCKS5 client ignores it for CONNECT. Reporting
// a fixed, well-formed IPv4 pair keeps the frame parseable, which is the part
// that actually matters.
func writeReply(w io.Writer, rep byte) error {
	_, err := w.Write([]byte{version, rep, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0})
	return err
}

// replyCodeForDialError maps a dial failure onto the closest RFC 1928 code.
//
// The mapping is three-way, and the split it refuses to guess at is the point.
// A failing dial here means one of two very different things — "the
// destination refused" or "the SSH transport is gone" — and they send the user
// to different machines. X'05' (connection refused) asserts the first. Emitting
// it for a dead tunnel tells the user to go check a destination that is fine,
// from the one vantage point where they cannot see that their tunnel is what
// broke.
//
// So X'05' is emitted only on an explicit claim from the dialer:
//
//   - ErrDestinationRefused → X'05'. The caller observed a refusal of *that
//     target* (in atterm, an *ssh.OpenChannelError: the remote sshd declined
//     the channel and the tunnel is healthy). X'05' is the honest code, and
//     withholding it would be its own lie — "something went wrong" for the most
//     ordinary outcome there is, nothing listening on that port.
//   - a timeout → X'04' (host unreachable). Still the closest RFC 1928 has.
//   - everything else → X'01' (general SOCKS server failure). Transport death
//     and context cancellation land here. X'01' says "this proxy could not do
//     it", which is true and points at the proxy rather than at an innocent
//     destination.
//
// Order matters. context.DeadlineExceeded reports Timeout() == true, so the
// context check has to come before the timeout check or a cancelled dial would
// be reported as an unreachable host — blaming the destination for a decision
// made on this side.
//
// The classification is by sentinel and by type, never by error text: the SSH
// refusal string literally contains "connection refused", so a text match would
// hand X'05' to exactly the transport failures this function exists to keep it
// away from.
func replyCodeForDialError(err error) byte {
	if errors.Is(err, ErrDestinationRefused) {
		return repConnectionRefused
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return repGeneralFailure
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return repHostUnreachable
	}
	return repGeneralFailure
}

// splice copies bytes both ways until either side ends, then closes both. It
// returns only once both copies are done, so no goroutine outlives the
// connection that spawned it.
func splice(a, b net.Conn) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(a, b)
		_ = a.Close()
		_ = b.Close()
	}()
	_, _ = io.Copy(b, a)
	_ = b.Close()
	_ = a.Close()
	<-done
}
