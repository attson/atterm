package socks5

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- harness ----------------------------------------------------------------

// testServer is a Serve loop on a real loopback listener with an injected
// dialer. No SSH is involved anywhere in this file: the point of the injected
// Dialer is that the protocol code can be exercised with nothing but memory.
type testServer struct {
	addr string

	// lastAddr is the address string handed to the dialer by the most recent
	// CONNECT, which is how the address-type tests check parsing.
	lastAddr atomic.Value // string
	dials    atomic.Int64
}

// echoDialer returns a Dialer whose connections echo whatever is written to
// them, built on net.Pipe so no port, no kernel socket and no route is
// involved in a "successful CONNECT".
func (s *testServer) echoDialer() Dialer {
	return func(network, addr string) (net.Conn, error) {
		s.lastAddr.Store(addr)
		s.dials.Add(1)
		client, server := net.Pipe()
		go func() { _, _ = io.Copy(server, server); _ = server.Close() }()
		return client, nil
	}
}

func (s *testServer) requested() string {
	v, _ := s.lastAddr.Load().(string)
	return v
}

func startServer(t *testing.T, dial Dialer) *testServer {
	t.Helper()
	s := &testServer{}
	if dial == nil {
		dial = s.echoDialer()
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s.addr = ln.Addr().String()
	go func() { _ = Serve(ln, dial) }()
	return s
}

// dialServer opens a client connection with a deadline on it, so a server bug
// that never answers fails the test instead of hanging the whole run.
func dialServer(t *testing.T, s *testServer) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", s.addr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	return c
}

func mustWrite(t *testing.T, c net.Conn, b []byte) {
	t.Helper()
	if _, err := c.Write(b); err != nil {
		t.Fatalf("write %v: %v", b, err)
	}
}

// negotiateNoAuth does the method-selection half and asserts NO AUTHENTICATION
// was chosen, so the CONNECT tests can start from a settled handshake.
func negotiateNoAuth(t *testing.T, c net.Conn) {
	t.Helper()
	mustWrite(t, c, []byte{version, 1, methodNoAuth})
	var reply [2]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		t.Fatalf("read method reply: %v", err)
	}
	if reply[0] != version || reply[1] != methodNoAuth {
		t.Fatalf("method reply = % x, want %02x %02x", reply, version, methodNoAuth)
	}
}

// parsedReply is a decoded SOCKS5 reply. Every test that expects a reply
// decodes it through here rather than eyeballing a byte slice, because a reply
// a real client cannot parse is exactly the bug these tests exist to catch.
type parsedReply struct {
	ver  byte
	rep  byte
	rsv  byte
	atyp byte
	addr string
	port uint16
}

// readReply reads one reply the way a client would: fixed header, then an
// address whose length depends on ATYP, then the port. A malformed reply shows
// up here as a read error or a wrong length, not as a silently accepted blob.
func readReply(t *testing.T, c net.Conn) parsedReply {
	t.Helper()
	var head [4]byte
	if _, err := io.ReadFull(c, head[:]); err != nil {
		t.Fatalf("read reply header: %v", err)
	}
	r := parsedReply{ver: head[0], rep: head[1], rsv: head[2], atyp: head[3]}
	var addrLen int
	switch r.atyp {
	case atypIPv4:
		addrLen = net.IPv4len
	case atypIPv6:
		addrLen = net.IPv6len
	case atypDomain:
		var n [1]byte
		if _, err := io.ReadFull(c, n[:]); err != nil {
			t.Fatalf("read reply domain length: %v", err)
		}
		addrLen = int(n[0])
	default:
		t.Fatalf("reply carries an unknown address type %#x (a real client cannot parse this)", r.atyp)
	}
	buf := make([]byte, addrLen+2)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read reply address+port: %v", err)
	}
	if r.atyp == atypDomain {
		r.addr = string(buf[:addrLen])
	} else {
		r.addr = net.IP(buf[:addrLen]).String()
	}
	r.port = binary.BigEndian.Uint16(buf[addrLen:])
	return r
}

// assertWellFormed checks the invariants a reply must hold whatever it is
// reporting: version 5 and a zeroed reserved byte. A "well-formed X'07'" that
// gets either wrong is not something a client can act on.
func (r parsedReply) assertWellFormed(t *testing.T) {
	t.Helper()
	if r.ver != version {
		t.Fatalf("reply version = %#x, want %#x", r.ver, version)
	}
	if r.rsv != 0 {
		t.Fatalf("reply reserved byte = %#x, want 0", r.rsv)
	}
}

// connectRequest builds a CONNECT request for a raw address body.
func connectRequest(atyp byte, addrBody []byte, port uint16) []byte {
	req := []byte{version, cmdConnect, 0, atyp}
	req = append(req, addrBody...)
	return binary.BigEndian.AppendUint16(req, port)
}

// expectServerClosed asserts the server hangs up rather than parking the
// connection (and its goroutine) forever. Reaching the read deadline here
// means the server is holding a connection it has already answered.
func expectServerClosed(t *testing.T, c net.Conn, within time.Duration) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(within))
	_, err := c.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("server kept the connection open and readable after it was done with it")
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatalf("server never closed the connection (read timed out after %v)", within)
	}
}

// --- successful CONNECT ------------------------------------------------------

func TestConnectIPv4CarriesBytesBothWays(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	mustWrite(t, c, connectRequest(atypIPv4, []byte{93, 184, 216, 34}, 443))
	rep := readReply(t, c)
	rep.assertWellFormed(t)
	if rep.rep != repSuccess {
		t.Fatalf("CONNECT reply = %#x, want success", rep.rep)
	}
	if got := s.requested(); got != "93.184.216.34:443" {
		t.Fatalf("dialer got %q, want 93.184.216.34:443", got)
	}
	assertEchoes(t, c, "ipv4 payload")
}

func TestConnectIPv6CarriesBytesBothWays(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	ip := net.ParseIP("2606:2800:220:1:248:1893:25c8:1946").To16()
	mustWrite(t, c, connectRequest(atypIPv6, ip, 8080))
	rep := readReply(t, c)
	rep.assertWellFormed(t)
	if rep.rep != repSuccess {
		t.Fatalf("CONNECT reply = %#x, want success", rep.rep)
	}
	// net.JoinHostPort must bracket the v6 literal, or the dialer is handed
	// something unparseable.
	if got := s.requested(); got != "[2606:2800:220:1:248:1893:25c8:1946]:8080" {
		t.Fatalf("dialer got %q, want the bracketed v6 form", got)
	}
	assertEchoes(t, c, "ipv6 payload")
}

func TestConnectDomainCarriesBytesBothWays(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	const host = "example.internal"
	body := append([]byte{byte(len(host))}, host...)
	mustWrite(t, c, connectRequest(atypDomain, body, 5432))
	rep := readReply(t, c)
	rep.assertWellFormed(t)
	if rep.rep != repSuccess {
		t.Fatalf("CONNECT reply = %#x, want success", rep.rep)
	}
	// The name must be passed through unresolved: resolving it here would
	// resolve it on the *wrong side* of the tunnel.
	if got := s.requested(); got != host+":5432" {
		t.Fatalf("dialer got %q, want %s:5432", got, host)
	}
	assertEchoes(t, c, "domain payload")
}

// assertEchoes proves the splice is live in both directions after the reply —
// a server that replies success and then forgets to copy bytes passes every
// header assertion above.
func assertEchoes(t *testing.T, c net.Conn, msg string) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	mustWrite(t, c, []byte(msg))
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != msg {
		t.Fatalf("echo = %q, want %q", buf, msg)
	}
}

// TestConnectOutlivesTheHandshakeDeadline is the bug the deadline strategy
// invites: a read deadline set for the handshake and never cleared kills an
// established proxied connection the moment it idles past it. handshakeTimeout
// is shortened here so an uncleared deadline shows up as a dead splice.
func TestConnectOutlivesTheHandshakeDeadline(t *testing.T) {
	restore := shortHandshakeTimeout(t, 150*time.Millisecond)
	defer restore()

	s := startServer(t, nil)
	c := dialServer(t, s)
	negotiateNoAuth(t, c)
	mustWrite(t, c, connectRequest(atypIPv4, []byte{127, 0, 0, 1}, 1))
	if rep := readReply(t, c); rep.rep != repSuccess {
		t.Fatalf("CONNECT reply = %#x, want success", rep.rep)
	}

	time.Sleep(400 * time.Millisecond) // well past the handshake deadline
	assertEchoes(t, c, "still alive")
}

// --- method negotiation ------------------------------------------------------

// TestOnlyUsernamePasswordOffered: the only method we implement is NO
// AUTHENTICATION, so a client offering just username/password must be told
// X'FF' rather than being left guessing.
func TestOnlyUsernamePasswordOffered(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)

	mustWrite(t, c, []byte{version, 1, 0x02})
	var reply [2]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		t.Fatalf("read method reply: %v", err)
	}
	if reply[0] != version || reply[1] != methodNoAcceptable {
		t.Fatalf("method reply = % x, want %02x ff", reply, version)
	}
	expectServerClosed(t, c, 3*time.Second)
	if s.dials.Load() != 0 {
		t.Fatal("a rejected handshake must not dial anything")
	}
}

// TestZeroMethodsOffered: NMETHODS = 0 means the method list is empty, so
// there is nothing to accept. The parser must not read a negative/zero-length
// list into a panic, and must still answer X'FF'.
func TestZeroMethodsOffered(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)

	mustWrite(t, c, []byte{version, 0})
	var reply [2]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		t.Fatalf("read method reply: %v", err)
	}
	if reply[0] != version || reply[1] != methodNoAcceptable {
		t.Fatalf("method reply = % x, want %02x ff", reply, version)
	}
	expectServerClosed(t, c, 3*time.Second)
}

func TestManyMethodsIncludingNoAuth(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)

	mustWrite(t, c, []byte{version, 4, 0x02, 0x03, methodNoAuth, 0x80})
	var reply [2]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		t.Fatalf("read method reply: %v", err)
	}
	if reply[1] != methodNoAuth {
		t.Fatalf("method reply = % x, want no-auth selected", reply)
	}
}

// --- malformed input ---------------------------------------------------------

// TestWrongGreetingVersion: a SOCKS4 (or entirely non-SOCKS) client must be
// dropped, not fed a v5 reply it will misparse, and must not take the server
// down with it.
func TestWrongGreetingVersion(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)

	mustWrite(t, c, []byte{0x04, 1, 0x00})
	expectServerClosed(t, c, 3*time.Second)
	if s.dials.Load() != 0 {
		t.Fatal("a bad greeting must not dial anything")
	}
	assertServerStillServes(t, s)
}

func TestWrongRequestVersion(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	mustWrite(t, c, []byte{0x04, cmdConnect, 0, atypIPv4, 1, 2, 3, 4, 0, 80})
	expectServerClosed(t, c, 3*time.Second)
	if s.dials.Load() != 0 {
		t.Fatal("a bad request version must not dial anything")
	}
	assertServerStillServes(t, s)
}

// TestTruncatedDomainAddress: the length prefix claims more bytes than ever
// arrive and then the client hangs up. The classic form of this bug is
// allocating the claimed length and blocking on a read that never completes.
func TestTruncatedDomainAddress(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	// Claims 200 bytes of domain, sends 3, then closes.
	mustWrite(t, c, []byte{version, cmdConnect, 0, atypDomain, 200, 'a', 'b', 'c'})
	_ = c.Close()

	assertServerStillServes(t, s)
	if s.dials.Load() != 0 {
		t.Fatal("a truncated address must not reach the dialer")
	}
}

// TestTruncatedPort covers the other truncation point: a complete address
// followed by only one of the two port bytes.
func TestTruncatedPort(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	mustWrite(t, c, []byte{version, cmdConnect, 0, atypIPv4, 10, 0, 0, 1, 0x1f})
	_ = c.Close()

	assertServerStillServes(t, s)
	if s.dials.Load() != 0 {
		t.Fatal("a truncated port must not reach the dialer")
	}
}

// TestCloseMidHandshake hangs up at every prefix of a full exchange. Each one
// is a different read the server is sitting in.
func TestCloseMidHandshake(t *testing.T) {
	s := startServer(t, nil)
	prefixes := [][]byte{
		nil,
		{version},
		{version, 1},
		{version, 2, methodNoAuth},
	}
	for _, p := range prefixes {
		c, err := net.DialTimeout("tcp", s.addr, 3*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if len(p) > 0 {
			_, _ = c.Write(p)
		}
		_ = c.Close()
	}
	// And one that dies after a complete greeting, mid-request.
	c := dialServer(t, s)
	negotiateNoAuth(t, c)
	mustWrite(t, c, []byte{version, cmdConnect})
	_ = c.Close()

	assertServerStillServes(t, s)
}

// TestZeroLengthDomain: a domain of length 0 parses cleanly as far as framing
// goes but is not an address anyone can dial. It must be answered with a
// well-formed failure reply, not dialed as ":443" and not silently dropped.
func TestZeroLengthDomain(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	mustWrite(t, c, connectRequest(atypDomain, []byte{0}, 443))
	rep := readReply(t, c)
	rep.assertWellFormed(t)
	if rep.rep == repSuccess {
		t.Fatal("an empty domain name must not be reported as a successful CONNECT")
	}
	if s.dials.Load() != 0 {
		t.Fatal("an empty domain name must not reach the dialer")
	}
	expectServerClosed(t, c, 3*time.Second)
}

func TestUnknownAddressType(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	mustWrite(t, c, []byte{version, cmdConnect, 0, 0x09, 1, 2, 3, 4, 0, 80})
	rep := readReply(t, c)
	rep.assertWellFormed(t)
	if rep.rep != repAddrTypeNotSupported {
		t.Fatalf("reply = %#x, want %#x (address type not supported)", rep.rep, repAddrTypeNotSupported)
	}
	expectServerClosed(t, c, 3*time.Second)
}

// TestGarbageAfterGreeting throws bytes that are not a request at all at the
// request parser.
func TestGarbageAfterGreeting(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)
	negotiateNoAuth(t, c)
	mustWrite(t, c, []byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	expectServerClosed(t, c, 3*time.Second)
	assertServerStillServes(t, s)
}

// --- unsupported commands ----------------------------------------------------

// TestUnsupportedCommandsAreAnswered is the case where a silent hangup is
// indistinguishable from a broken server. RFC 1928 has X'07' for exactly this,
// and the reply has to survive being read *after* the server is done with the
// request.
func TestUnsupportedCommandsAreAnswered(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  byte
	}{
		{"BIND", cmdBind},
		{"UDP ASSOCIATE", cmdUDPAssociate},
		{"undefined command", 0x09},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := startServer(t, nil)
			c := dialServer(t, s)
			negotiateNoAuth(t, c)

			mustWrite(t, c, connectRequestCmd(tc.cmd, atypIPv4, []byte{127, 0, 0, 1}, 1080))
			// Deliberately pause before reading: the reply must be sitting in
			// the connection, not raced against a hangup that happened first.
			time.Sleep(50 * time.Millisecond)
			rep := readReply(t, c)
			rep.assertWellFormed(t)
			if rep.rep != repCommandNotSupported {
				t.Fatalf("reply = %#x, want %#x (command not supported)", rep.rep, repCommandNotSupported)
			}
			if s.dials.Load() != 0 {
				t.Fatal("an unsupported command must not dial anything")
			}
			assertServerStillServes(t, s)
		})
	}
}

// TestPipelinedRequestStillGetsItsReply: a client is allowed to send its
// greeting, its request and application payload in one go without waiting for
// either reply. When the request is then refused, the server is left holding
// unread bytes — and closing a TCP socket with unread data sends RST on Linux,
// which makes the peer discard the reply we just wrote. The refusal would
// reach the client as a silent disconnect, which is the exact failure mode
// X'07' exists to prevent.
func TestPipelinedRequestStillGetsItsReply(t *testing.T) {
	s := startServer(t, nil)
	c := dialServer(t, s)

	var pipelined []byte
	pipelined = append(pipelined, version, 1, methodNoAuth)
	pipelined = append(pipelined, connectRequestCmd(cmdBind, atypIPv4, []byte{127, 0, 0, 1}, 1080)...)
	pipelined = append(pipelined, []byte(strings.Repeat("payload sent without waiting for a reply", 20))...)
	mustWrite(t, c, pipelined)

	var methodReply [2]byte
	if _, err := io.ReadFull(c, methodReply[:]); err != nil {
		t.Fatalf("read method reply: %v", err)
	}
	rep := readReply(t, c)
	rep.assertWellFormed(t)
	if rep.rep != repCommandNotSupported {
		t.Fatalf("reply = %#x, want %#x (command not supported)", rep.rep, repCommandNotSupported)
	}
	expectServerClosed(t, c, 3*time.Second)
}

func connectRequestCmd(cmd, atyp byte, addrBody []byte, port uint16) []byte {
	req := []byte{version, cmd, 0, atyp}
	req = append(req, addrBody...)
	return binary.BigEndian.AppendUint16(req, port)
}

// --- dial failure ------------------------------------------------------------

// TestDialFailureReportsAFailureCode: replying success and then hanging up
// leaves a client convinced the tunnel is open. The failure has to be in the
// reply code.
//
// And an *unmarked* failure must be X'01', not X'05'. A dial failure that
// arrives with no claim attached can equally mean the SSH transport died, and
// X'05' would assert that the *destination* refused — sending the user to check
// the wrong machine, from the one vantage point where they cannot see that
// their tunnel is what broke.
//
// The error text below is the bait, and it is here deliberately: it says
// "connection refused" in so many words, because that is the remote sshd's
// wording. Only ErrDestinationRefused may produce X'05'; an implementation
// that reached for strings.Contains instead of the sentinel passes every other
// test in this file and fails this one.
func TestDialFailureReportsAFailureCode(t *testing.T) {
	s := startServer(t, func(network, addr string) (net.Conn, error) {
		return nil, errors.New("ssh: rejected: connect failed (connection refused)")
	})
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	mustWrite(t, c, connectRequest(atypIPv4, []byte{10, 1, 2, 3}, 5432))
	rep := readReply(t, c)
	rep.assertWellFormed(t)
	if rep.rep == repSuccess {
		t.Fatal("a failed dial was reported as a successful CONNECT")
	}
	if rep.rep == repConnectionRefused {
		t.Fatal("a dial failure must not be reported as X'05' connection refused: " +
			"this package cannot tell a refused destination from a dead ssh transport, " +
			"and claiming the former points the user at the wrong machine")
	}
	if rep.rep != repGeneralFailure {
		t.Fatalf("reply = %#x, want %#x (general SOCKS server failure)", rep.rep, repGeneralFailure)
	}
	expectServerClosed(t, c, 3*time.Second)
	assertServerStillServes(t, s)
}

// TestDialErrorsMapToThreeDistinctReplyCodes pins the whole mapping at once,
// on the wire, because the three codes are three different instructions to the
// user and collapsing any pair of them is a silent misdirection:
//
//   - X'05' sends them to the destination ("nothing is listening there").
//   - X'01' sends them to the proxy ("your tunnel is broken").
//   - X'04' says the path did not answer in time.
//
// Each row below fails if its branch is merged into the catch-all, so the
// function cannot be simplified back into "timed out vs everything else"
// without a red test. The context rows are the subtle ones:
// context.DeadlineExceeded satisfies net.Error with Timeout() == true, so if
// the context check is dropped or moved after the timeout check, a dial we
// cancelled ourselves gets reported as an unreachable host.
func TestDialErrorsMapToThreeDistinctReplyCodes(t *testing.T) {
	refusedByRemote := errors.New("ssh: rejected: connect failed (connection refused)")

	cases := []struct {
		name string
		err  error
		want byte
		why  string
	}{{
		name: "destination refused, wrapped by the dialer",
		err:  fmt.Errorf("%w: %w", ErrDestinationRefused, refusedByRemote),
		want: repConnectionRefused,
		why:  "the caller observed a refusal of this target; X'05' is the honest code and the user should look at the destination",
	}, {
		name: "bare ErrDestinationRefused",
		err:  ErrDestinationRefused,
		want: repConnectionRefused,
		why:  "the sentinel alone must be enough; not every caller has an underlying error to wrap",
	}, {
		name: "transport died",
		err:  refusedByRemote,
		want: repGeneralFailure,
		why:  "unmarked failures may be a dead tunnel, and its text says 'connection refused' — the trap for a text-matching implementation",
	}, {
		name: "context cancelled",
		err:  context.Canceled,
		want: repGeneralFailure,
		why:  "we stopped it, so nothing is known about the destination",
	}, {
		name: "context deadline exceeded",
		err:  context.DeadlineExceeded,
		want: repGeneralFailure,
		why:  "this is a Timeout() error, so it must be classified before the timeout branch or it becomes X'04'",
	}, {
		name: "wrapped context deadline",
		err:  fmt.Errorf("dialing: %w", context.DeadlineExceeded),
		want: repGeneralFailure,
		why:  "the check must be errors.Is, not ==; a dialer that adds context to the error still made this decision on our side",
	}, {
		name: "sentinel wrapping an opaque error",
		err:  fmt.Errorf("%w: boom", ErrDestinationRefused),
		want: repConnectionRefused,
		why:  "nothing but the sentinel can produce X'05' here, so this pins the sentinel rather than the underlying text",
	}, {
		name: "dial timed out",
		err:  timeoutErr{},
		want: repHostUnreachable,
		why:  "the closest RFC 1928 has for 'the path did not answer'",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err
			s := startServer(t, func(network, addr string) (net.Conn, error) {
				return nil, err
			})
			c := dialServer(t, s)
			negotiateNoAuth(t, c)

			mustWrite(t, c, connectRequest(atypIPv4, []byte{10, 1, 2, 3}, 5432))
			rep := readReply(t, c)
			rep.assertWellFormed(t)
			if rep.rep != tc.want {
				t.Fatalf("reply = %#x, want %#x: %s", rep.rep, tc.want, tc.why)
			}
			expectServerClosed(t, c, 3*time.Second)
			assertServerStillServes(t, s)
		})
	}
}

// TestDialTimeoutReportsAFailureCode pins that a dialer that times out is a
// failure reply too, not a connection parked forever waiting on it.
func TestDialTimeoutReportsAFailureCode(t *testing.T) {
	s := startServer(t, func(network, addr string) (net.Conn, error) {
		return nil, timeoutErr{}
	})
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	mustWrite(t, c, connectRequest(atypIPv4, []byte{10, 1, 2, 3}, 5432))
	rep := readReply(t, c)
	rep.assertWellFormed(t)
	if rep.rep == repSuccess {
		t.Fatal("a timed-out dial was reported as a successful CONNECT")
	}
}

// TestSlowDialStillGetsAReply: the dial happens with the handshake deadline
// still ticking, and dialing a dead host through an SSH channel is precisely
// the case that takes a while. If the deadline is not re-armed, the reply
// write fails and the client sees a bare disconnect instead of a failure code.
func TestSlowDialStillGetsAReply(t *testing.T) {
	restore := shortHandshakeTimeout(t, 150*time.Millisecond)
	defer restore()

	s := startServer(t, func(network, addr string) (net.Conn, error) {
		time.Sleep(300 * time.Millisecond) // longer than the whole handshake budget
		return nil, errors.New("no route to host")
	})
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	mustWrite(t, c, connectRequest(atypIPv4, []byte{10, 9, 9, 9}, 22))
	rep := readReply(t, c)
	rep.assertWellFormed(t)
	if rep.rep == repSuccess {
		t.Fatal("a failed dial was reported as a successful CONNECT")
	}
}

// TestSlowDialThatSucceedsStillGetsAReply is the same for the success side:
// the reply must go out and the splice must then be free of any deadline.
func TestSlowDialThatSucceedsStillGetsAReply(t *testing.T) {
	restore := shortHandshakeTimeout(t, 150*time.Millisecond)
	defer restore()

	s := &testServer{}
	echo := s.echoDialer()
	slow := startServer(t, func(network, addr string) (net.Conn, error) {
		time.Sleep(300 * time.Millisecond)
		return echo(network, addr)
	})
	c := dialServer(t, slow)
	negotiateNoAuth(t, c)

	mustWrite(t, c, connectRequest(atypIPv4, []byte{10, 9, 9, 9}, 22))
	rep := readReply(t, c)
	rep.assertWellFormed(t)
	if rep.rep != repSuccess {
		t.Fatalf("CONNECT reply = %#x, want success", rep.rep)
	}
	assertEchoes(t, c, "after a slow dial")
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// TestDialerReturningNilConnAndNilError is defence against the injected
// dialer misbehaving: the server must not dereference a nil net.Conn.
func TestDialerReturningNilConnAndNilError(t *testing.T) {
	s := startServer(t, func(network, addr string) (net.Conn, error) {
		return nil, nil
	})
	c := dialServer(t, s)
	negotiateNoAuth(t, c)

	mustWrite(t, c, connectRequest(atypIPv4, []byte{10, 1, 2, 3}, 5432))
	rep := readReply(t, c)
	rep.assertWellFormed(t)
	if rep.rep == repSuccess {
		t.Fatal("a nil connection was reported as a successful CONNECT")
	}
	assertServerStillServes(t, s)
}

// --- deadlines and lifetime --------------------------------------------------

// TestSilentClientIsTimedOut: a client that connects and sends nothing pins a
// goroutine and a socket for as long as the server is willing to wait. With no
// read deadline that is forever, and a few hundred of them is a resource leak
// an attacker on loopback can trigger for free.
func TestSilentClientIsTimedOut(t *testing.T) {
	restore := shortHandshakeTimeout(t, 150*time.Millisecond)
	defer restore()

	s := startServer(t, nil)
	c := dialServer(t, s)
	// Say nothing at all.
	expectServerClosed(t, c, 3*time.Second)
}

// TestSlowClientBetweenGreetingAndRequestIsTimedOut covers the second read:
// completing the greeting must not buy an unbounded wait for the request.
func TestSlowClientBetweenGreetingAndRequestIsTimedOut(t *testing.T) {
	restore := shortHandshakeTimeout(t, 150*time.Millisecond)
	defer restore()

	s := startServer(t, nil)
	c := dialServer(t, s)
	negotiateNoAuth(t, c)
	expectServerClosed(t, c, 3*time.Second)
}

// TestSilentClientsDoNotAccumulateGoroutines is the leak assertion the
// per-connection timeout exists for: many silent clients, all of which must be
// reaped without help from the caller.
func TestSilentClientsDoNotAccumulateGoroutines(t *testing.T) {
	restore := shortHandshakeTimeout(t, 100*time.Millisecond)
	defer restore()

	s := startServer(t, nil)
	before := runtime.NumGoroutine()
	var conns []net.Conn
	for i := 0; i < 30; i++ {
		c, err := net.DialTimeout("tcp", s.addr, 3*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+10 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before+10 {
		t.Fatalf("goroutines grew from %d to %d: silent clients are not being reaped", before, got)
	}
	for _, c := range conns {
		_ = c.Close()
	}
}

// TestServeReturnsWhenTheListenerCloses pins the lifetime contract the tunnel
// manager depends on: the manager owns the listener, and closing it is how a
// stopped rule ends the Serve loop.
func TestServeReturnsWhenTheListenerCloses(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Serve(ln, func(string, string) (net.Conn, error) { return nil, errors.New("no") }) }()

	_ = ln.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Serve should report why it stopped accepting")
		}
		if !strings.Contains(err.Error(), "closed") {
			t.Fatalf("Serve returned %v, want the listener's close error", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after its listener was closed")
	}
}

// TestServeConnClosesTheRemoteWhenTheClientGoesAway: the dialed connection is
// the server's to clean up. Leaking it would leak an SSH channel per proxied
// connection in the real wiring.
func TestServeConnClosesTheRemoteWhenTheClientGoesAway(t *testing.T) {
	remoteClosed := make(chan struct{})
	s := startServer(t, func(network, addr string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			_, _ = io.Copy(io.Discard, server)
			_ = server.Close()
			close(remoteClosed)
		}()
		return client, nil
	})
	c := dialServer(t, s)
	negotiateNoAuth(t, c)
	mustWrite(t, c, connectRequest(atypIPv4, []byte{10, 0, 0, 1}, 80))
	if rep := readReply(t, c); rep.rep != repSuccess {
		t.Fatalf("CONNECT reply = %#x, want success", rep.rep)
	}
	_ = c.Close()

	select {
	case <-remoteClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("the dialed connection outlived the client that asked for it")
	}
}

// shortHandshakeTimeout shrinks the handshake deadline for one test.
func shortHandshakeTimeout(t *testing.T, d time.Duration) func() {
	t.Helper()
	old := handshakeTimeoutNanos.Swap(int64(d))
	return func() { handshakeTimeoutNanos.Store(old) }
}

// assertServerStillServes is the "did not panic / did not wedge" assertion:
// after whatever abuse a test just committed, a well-formed client still gets
// served. A panic in a connection goroutine takes the whole process down, so
// this reaching its assertions at all is itself part of the check.
func assertServerStillServes(t *testing.T, s *testServer) {
	t.Helper()
	c, err := net.DialTimeout("tcp", s.addr, 3*time.Second)
	if err != nil {
		t.Fatalf("server stopped accepting: %v", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	mustWrite(t, c, []byte{version, 1, methodNoAuth})
	var reply [2]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		t.Fatalf("server stopped answering: %v", err)
	}
	if reply[1] != methodNoAuth {
		t.Fatalf("server answered %v after the previous connection", reply)
	}
}
