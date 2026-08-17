package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/sshclient"
)

// Jump-host chains (roadmap item 27).
//
// A chain is a fold over sshclient.Config.Via: hop 1 is dialled from this
// machine, hop 2 through hop 1, and the target through the last hop. The two
// decisions that shape everything here are in the design (§5.1, §5.2):
//
//   - Every hop must itself be a saved host. A bastion needs its own
//     authentication, and the alternatives are worse: reusing the *target's*
//     credential sends the target machine's password or key to a different
//     machine, and prompting mid-connect either asks every time or quietly
//     manufactures a host record the user never reviewed. Looking the hop up
//     in the saved list yields credential, username and port at once, from
//     somewhere the user has already seen.
//   - Every hop's host key is verified on its own, and an unknown one names
//     which hop it belongs to. Verifying only the target would let a
//     substituted bastion sit in the middle while the user reads "the
//     target's fingerprint is unchanged".
//
// Resolution is entirely static and runs to completion *before* the first
// dial, so an unresolvable hop, a cycle or an over-deep chain costs nothing —
// the same discipline the ssh_config parser's Include cycle detection follows.

// maxJumpDepth caps how many jump hosts one chain may have (the target itself
// is not counted). Ten is far past any real topology; the cap exists so a
// mis-typed config fails with a sentence rather than by opening logins until
// something else stops it.
const maxJumpDepth = 10

// jumpDialTimeout bounds each hop's dial+handshake individually. A chain does
// not get a longer budget per hop than a direct connection does.
const jumpDialTimeout = 15 * time.Second

// jumpChain owns every connection opened to reach a target, target last.
//
// Closing the target does not close the connections behind it — each hop is a
// separate SSH connection whose only relationship to the next is that it
// carries its transport — so the whole chain has to be owned by one handle.
type jumpChain struct {
	conns []*sshclient.Conn

	closeOnce sync.Once
}

// Target returns the connection to the destination host: the last one opened.
// Nil for an empty chain.
func (c *jumpChain) Target() *sshclient.Conn {
	if c == nil || len(c.conns) == 0 {
		return nil
	}
	return c.conns[len(c.conns)-1]
}

// Close closes the whole chain, target first and then back down the hops. The
// order matters: a hop closed first would drop the transport under the
// connection riding it, turning an orderly close into a dropped connection for
// everything downstream. Safe to call more than once; returns the first error.
func (c *jumpChain) Close() error {
	if c == nil {
		return nil
	}
	var firstErr error
	c.closeOnce.Do(func() {
		for i := len(c.conns) - 1; i >= 0; i-- {
			if err := c.conns[i].Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	})
	return firstErr
}

// dialThroughJumps opens every connection needed to reach h and returns them as
// one handle, h's own connection last. A host with no ProxyJump yields a
// one-element chain (a plain direct connection), so callers do not need a
// second code path for the common case.
//
// acceptHostKey is the TOFU retry flag from the frontend, exactly as
// NewSshSession uses it.
func (a *App) dialThroughJumps(ctx context.Context, h SSHHost, acceptHostKey bool) (*jumpChain, error) {
	chain, err := a.dialJumpHops(ctx, h, acceptHostKey)
	if err != nil {
		return nil, err
	}
	// A chain of length 0 means this host is dialled directly; there is no hop
	// sequence to disambiguate, so its TOFU prompt must read exactly as it did
	// before jump hosts existed.
	hopIndex := 0
	if len(chain.conns) > 0 {
		hopIndex = len(chain.conns) + 1
	}
	conn, err := a.dialJumpHop(ctx, h, hopIndex, chain.Target(), acceptHostKey, true)
	if err != nil {
		_ = chain.Close()
		return nil, err
	}
	chain.conns = append(chain.conns, conn)
	return chain, nil
}

// dialJumpHops opens the connections to h's jump hosts and stops there, without
// touching h itself. The terminal path needs this: its last step is not a
// plain connection but a connection with a shell on it, which it opens with
// sshclient.Config.Via set to Target() of what this returns.
//
// On any failure every connection already opened is closed before returning —
// a half-built chain left behind is a session hanging on a bastion the user
// cannot see, let alone close.
func (a *App) dialJumpHops(ctx context.Context, h SSHHost, acceptHostKey bool) (*jumpChain, error) {
	hops, err := resolveJumpHops(h, a.ListSSHHosts())
	if err != nil {
		return nil, err
	}
	chain := &jumpChain{}
	for i, hop := range hops {
		conn, err := a.dialJumpHop(ctx, hop, i+1, chain.Target(), acceptHostKey, false)
		if err != nil {
			_ = chain.Close()
			return nil, err
		}
		chain.conns = append(chain.conns, conn)
	}
	return chain, nil
}

// dialJumpHop opens one link of the chain: hop's *own* credential, hop's own
// host-key check, over via (nil for the first hop, which is dialled from this
// machine).
//
// isTarget only changes how errors are worded and whether the missing-credential
// sentinels are passed through untouched: the frontend reacts to a bare
// errCredentialMissing by prompting for the host the user asked to connect,
// which is the right thing for the target and the wrong thing for a bastion —
// there the user has to go and fill in that *other* host's credential, so the
// message has to say which.
func (a *App) dialJumpHop(ctx context.Context, h SSHHost, hopIndex int, via *sshclient.Conn, acceptHostKey, isTarget bool) (*sshclient.Conn, error) {
	name := sshHostLabel(h)

	// The hop's own credential. Nothing from the target is in scope here: a
	// bastion is a different machine, and handing it the target's password
	// would be a credential leak dressed up as a shortcut.
	auth, err := sshAuthForHost(h)
	if err != nil {
		if isTarget {
			return nil, err // bare sentinel: the frontend prompts for this host
		}
		return nil, fmt.Errorf("jump host %q (hop %d) has no usable credential (%s); "+
			"open that host and supply one", name, hopIndex, err)
	}

	var unknown *HostKeyUnknownError
	cb := sshclient.KnownHostsCallback(a.knownHostsPath(), func(host, fp string) bool {
		if acceptHostKey {
			return true // user already confirmed in the TOFU dialog
		}
		unknown = &HostKeyUnknownError{
			Fingerprint: fp, Host: host,
			HopIndex: hopIndex, HopName: name,
		}
		return false
	})

	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := sshclient.DialConn(ctx, sshclient.Config{
		Host: h.Host, Port: h.Port, User: h.User,
		Auth:      auth,
		HostKeyCb: cb,
		Timeout:   jumpDialTimeout,
		Via:       via,
	})
	if err != nil {
		if unknown != nil {
			return nil, unknown // typed → frontend shows the fingerprint *and* the hop
		}
		if isTarget && hopIndex == 0 {
			return nil, err // direct connection: no chain to blame
		}
		// Which machine failed is the whole message: "connection refused" on a
		// four-hop chain says nothing, and pointing the user at the wrong host
		// is worse than saying nothing.
		role := fmt.Sprintf("jump host %q (hop %d)", name, hopIndex)
		if isTarget {
			role = fmt.Sprintf("%q (the destination, hop %d)", name, hopIndex)
		}
		return nil, fmt.Errorf("cannot reach %s: %w", role, err)
	}
	return conn, nil
}

// resolveJumpHops expands h's ProxyJump into the hops to dial, in dial order,
// without h itself. It touches nothing but the saved host list, so every
// refusal below happens before a single connection exists.
func resolveJumpHops(h SSHHost, hosts []SSHHost) ([]SSHHost, error) {
	var out []SSHHost
	if err := appendJumpHops(h, hosts, []SSHHost{h}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// appendJumpHops walks h's ProxyJump depth-first: a hop's own jump hosts come
// before it, because they are how *it* is reached. path is the stack of hosts
// currently being resolved, which is what makes a cycle visible.
func appendJumpHops(h SSHHost, hosts []SSHHost, path []SSHHost, out *[]SSHHost) error {
	for _, elem := range splitProxyJump(h.ProxyJump) {
		hop, ok := findJumpHost(elem, hosts)
		if !ok {
			return fmt.Errorf(
				"%q is reached through jump host %q, but %q is not a saved host in atterm; "+
					"add it as a host (with its own credential) first — atterm will not send %q's "+
					"credential to a different machine",
				sshHostLabel(h), elem, elem, sshHostLabel(h))
		}
		if i := indexOfHost(path, hop); i >= 0 {
			return fmt.Errorf("the jump-host chain loops: %s", describeCycle(path[i:], hop))
		}
		if len(path) > maxJumpDepth {
			return jumpTooDeepError()
		}
		if err := appendJumpHops(hop, hosts, append(append([]SSHHost{}, path...), hop), out); err != nil {
			return err
		}
		*out = append(*out, hop)
		if len(*out) > maxJumpDepth {
			return jumpTooDeepError()
		}
	}
	return nil
}

func jumpTooDeepError() error {
	return fmt.Errorf("the jump-host chain is longer than %d hops; "+
		"atterm refuses to build it (check the ProxyJump entries of the hosts involved)", maxJumpDepth)
}

// describeCycle renders the loop as the path that closed it, e.g. "a → b → a".
func describeCycle(path []SSHHost, repeat SSHHost) string {
	names := make([]string, 0, len(path)+1)
	for _, h := range path {
		names = append(names, sshHostLabel(h))
	}
	return strings.Join(append(names, sshHostLabel(repeat)), " → ")
}

func indexOfHost(path []SSHHost, h SSHHost) int {
	key := hostIdentity(h)
	for i, p := range path {
		if hostIdentity(p) == key {
			return i
		}
	}
	return -1
}

// hostIdentity is "the same saved host" for cycle detection. ID is the real
// identity, but a host list assembled without AddSSHHost (a test, an inbound
// sync mid-write) can carry empty IDs, and a cycle that went undetected there
// would be found only by the depth cap — with a message about depth for what is
// actually a loop.
func hostIdentity(h SSHHost) string {
	if h.ID != "" {
		return "id:" + h.ID
	}
	return "addr:" + strings.ToLower(strings.TrimSpace(h.Alias)) + "\x00" +
		strings.ToLower(strings.TrimSpace(h.Host)) + "\x00" + strings.TrimSpace(h.Port)
}

// splitProxyJump splits a ProxyJump value into its elements, left to right —
// the order they are traversed in.
func splitProxyJump(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// findJumpHost resolves one ProxyJump element against the saved hosts, by alias
// first and hostname second.
//
// A `user@host:port` element is parsed *only* to get the name to match on. The
// user and port in it are deliberately dropped: they cannot conjure a host
// record, and the saved host they match is where the username, the port and —
// the reason for all of this — the credential come from.
func findJumpHost(elem string, hosts []SSHHost) (SSHHost, bool) {
	name := jumpElementName(elem)
	if name == "" {
		return SSHHost{}, false
	}
	for _, h := range hosts {
		if h.Alias != "" && strings.EqualFold(strings.TrimSpace(h.Alias), name) {
			return h, true
		}
	}
	for _, h := range hosts {
		if strings.EqualFold(strings.TrimSpace(h.Host), name) {
			return h, true
		}
	}
	return SSHHost{}, false
}

// jumpElementName strips the optional user@ prefix and :port suffix from one
// ProxyJump element, leaving the host name to match on.
func jumpElementName(elem string) string {
	s := strings.TrimSpace(elem)
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	switch {
	case strings.HasPrefix(s, "["): // [::1]:2222 — bracketed IPv6, with or without a port
		if end := strings.Index(s, "]"); end > 0 {
			return s[1:end]
		}
	case strings.Count(s, ":") == 1: // host:port
		return s[:strings.Index(s, ":")]
	}
	// Anything else with several colons is a bare IPv6 literal, which cannot
	// carry a port without brackets — so none of it is a port.
	return s
}

// sshHostLabel is how a host is named to the user: its alias when it has one,
// otherwise its hostname. Aliases are what the user typed in ProxyJump, so an
// error that used the hostname instead would not match anything they can see.
func sshHostLabel(h SSHHost) string {
	if s := strings.TrimSpace(h.Alias); s != "" {
		return s
	}
	return strings.TrimSpace(h.Host)
}
