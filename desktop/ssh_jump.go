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

// jumpKeepalive is how often each connection dialled by dialJumpHop pings its
// peer: every hop, plus the destination when it comes through dialThroughJumps
// (the tunnel path). The terminal path's destination is not one of these — it
// is opened by sshclient.Dial, which leaves Config.Keepalive unset and so takes
// DialConn's own 30s default. The two numbers are the same today, but a test
// that shortens this one does not shorten that one.
//
// It bounds how long a dropped connection can go unnoticed, which is what the
// tunnel path depends on: a tunnel's only notice that its connection died is
// Conn.Done, and a failed keepalive ping is what closes it. Without that an
// *idle* tunnel would keep reporting itself as running. Tests shorten this so a
// drop is observable in a test's lifetime.
var jumpKeepalive = 30 * time.Second

// acceptedHostKey is the one host key the user accepted in the TOFU dialog.
// Its zero value accepts nothing, which is what every caller that has not been
// through a dialog must pass.
//
// It replaces a plain "accept unknown keys" bool, and the difference is not
// cosmetic. sshclient.KnownHostsCallback does not merely allow an accepted key
// through: handleUnknown *appends it to known_hosts* the moment the callback
// says yes. So a bool that accepts the next unknown key on a chain would
// persist keys for hops the user was never shown, and the connection after that
// would prompt for nothing at all — the substitution becomes invisible
// precisely because it was recorded as trusted.
//
// The pair is keyed on the hostname the callback is handed (the known_hosts
// key: "host" or "[host]:port"), never on the hop's alias: an alias is
// user-editable and one bastion can legitimately appear twice in a chain, so
// matching on it would let a *different* hop that happens to present the same
// key take an acceptance the user granted elsewhere.
type acceptedHostKey struct {
	Host        string
	Fingerprint string
}

// accepts reports whether this is exactly the key the user agreed to.
func (k acceptedHostKey) accepts(host, fingerprint string) bool {
	return k.Fingerprint != "" && k.Fingerprint == fingerprint && k.Host == host
}

// jumpChain owns every connection opened to reach a target, target last.
//
// Closing the target does not close the connections behind it — each hop is a
// separate SSH connection whose only relationship to the next is that it
// carries its transport — so the whole chain has to be owned by one handle.
type jumpChain struct {
	conns []*sshclient.Conn
	// hasTarget distinguishes a complete chain from the hops-only chain
	// dialJumpHops returns, so targetHopIndex answers the same question in
	// both states instead of meaning something different depending on which
	// one the caller happens to be holding.
	hasTarget bool

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

// targetHopIndex is the hop number the destination carries in a
// HostKeyUnknownError: the hops in dial order, target last. 0 when there are no
// hops, meaning the connection is direct and there is nothing to disambiguate —
// a TOFU prompt for it must read exactly as it did before jump hosts existed.
//
// It answers the same on a hops-only chain (target not dialled yet) and on a
// complete one, so the terminal path — which dials its own last link, with a
// PTY on it — can number the target the way this file does instead of
// recomputing the arithmetic and drifting from it.
func (c *jumpChain) targetHopIndex() int {
	if c == nil {
		return 0
	}
	hops := len(c.conns)
	if c.hasTarget {
		hops--
	}
	if hops == 0 {
		return 0
	}
	return hops + 1
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
// second code path for the common case — which, once the terminal and tunnel
// paths route through here, is nearly every connection the app makes.
//
// accepted is the single host key the user confirmed in the TOFU dialog, echoed
// back from the *HostKeyUnknownError that produced the dialog. The zero value
// (accept nothing) is what a first attempt passes.
func (a *App) dialThroughJumps(ctx context.Context, h SSHHost, accepted acceptedHostKey) (*jumpChain, error) {
	// The destination's own credential is checked before any hop is dialled:
	// discovering it is missing after logging into three bastions spends every
	// side effect the static checks exist to avoid.
	if _, err := sshAuthForHost(h); err != nil {
		return nil, err // bare sentinel: the frontend prompts for this host
	}
	chain, err := a.dialJumpHops(ctx, h, accepted)
	if err != nil {
		return nil, err
	}
	conn, err := a.dialJumpHop(ctx, h, chain.targetHopIndex(), chain.Target(), accepted, true)
	if err != nil {
		_ = chain.Close()
		return nil, err
	}
	chain.conns = append(chain.conns, conn)
	chain.hasTarget = true
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
func (a *App) dialJumpHops(ctx context.Context, h SSHHost, accepted acceptedHostKey) (*jumpChain, error) {
	hops, err := resolveJumpHops(h, a.ListSSHHosts())
	if err != nil {
		return nil, err
	}
	// Every hop is vetted before the first dial, for the same reason the cycle
	// and depth checks are: a hop that cannot be used must not be discovered
	// after the hops in front of it have really logged in.
	for i, hop := range hops {
		// A hop carrying a ProxyCommand is a machine atterm refuses to dial at
		// all, in those words, the moment the user clicks it themselves
		// (hostRunsProxyCommand). Dialling its HostName:Port because somebody
		// else named it as a jump host would break that promise in the one
		// place the user cannot see it happening. This check lives here rather
		// than at the gate on purpose: §5.4 keeps the gate to exactly two call
		// sites, and this is not a gate — it is the chain refusing to build.
		if hop.ProxyCommand != "" {
			return nil, jumpProxyCommandError(hop, i+1)
		}
		// A hop that is saved but has no stored credential is the other way to
		// be unusable.
		if _, err := sshAuthForHost(hop); err != nil {
			return nil, jumpCredentialError(hop, i+1, err)
		}
	}
	chain := &jumpChain{}
	for i, hop := range hops {
		conn, err := a.dialJumpHop(ctx, hop, i+1, chain.Target(), accepted, false)
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
func (a *App) dialJumpHop(ctx context.Context, h SSHHost, hopIndex int, via *sshclient.Conn, accepted acceptedHostKey, isTarget bool) (*sshclient.Conn, error) {
	name := sshHostLabel(h)

	// The hop's own credential. Nothing from the target is in scope here: a
	// bastion is a different machine, and handing it the target's password
	// would be a credential leak dressed up as a shortcut.
	auth, err := sshAuthForHost(h)
	if err != nil {
		if isTarget {
			return nil, err // bare sentinel: the frontend prompts for this host
		}
		return nil, jumpCredentialError(h, hopIndex, err)
	}

	var unknown *HostKeyUnknownError
	cb := sshclient.KnownHostsCallback(a.knownHostsPath(), func(host, fp string) bool {
		// Only the exact key the user was shown and agreed to. Anything else
		// unknown on this chain stops here and comes back as its own prompt,
		// naming its own hop — see acceptedHostKey for why a blanket accept
		// would quietly write a stranger's key into known_hosts.
		if accepted.accepts(host, fp) {
			return true
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
		Keepalive: jumpKeepalive,
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

// jumpProxyCommandError words a hop that is configured with a ProxyCommand. It
// is a hop-specific message rather than hostRunsProxyCommand's, for the same
// reason jumpCredentialError is: that one says "this host cannot be connected
// directly" about the host the user asked for, and here the unusable machine is
// a *different* one, several steps into a chain the user may not have thought
// about since they imported it. Naming the hop and its index is the only way
// the sentence points at something they can act on.
func jumpProxyCommandError(h SSHHost, hopIndex int) error {
	return fmt.Errorf("jump host %q (hop %d) is configured with a ProxyCommand (%q); "+
		"atterm never runs that command, so it cannot be used as a jump host",
		sshHostLabel(h), hopIndex, h.ProxyCommand)
}

// jumpCredentialError words a hop's missing credential. It deliberately does
// not return the bare errCredentialMissing / errKeyMissing sentinel the target
// path returns: the frontend answers that sentinel by prompting for the host
// the user asked to connect, and here the credential that is missing belongs to
// a *different* host, which the user has to go and fill in.
func jumpCredentialError(h SSHHost, hopIndex int, err error) error {
	return fmt.Errorf("jump host %q (hop %d) has no usable credential (%s); "+
		"open that host and supply one", sshHostLabel(h), hopIndex, err)
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
// A `user@host:port` element is parsed *only* to find which saved host is
// meant. Neither half is ever used to build a connection: the saved host is
// where the username, the port and — the reason for all of this — the
// credential come from.
//
// The port does get one job: preference. Two saved records for the same
// hostname on different ports are a real configuration (a container and its
// host, an sshd on 2222), and silently taking whichever was saved first would
// send the connection to a machine the element named a different port for. So
// an element carrying a port matches a record on that port when one exists, and
// otherwise falls back to matching on the name alone — which is still a saved
// host, never a fabricated one.
func findJumpHost(elem string, hosts []SSHHost) (SSHHost, bool) {
	name, port := jumpElementNameAndPort(elem)
	if name == "" {
		return SSHHost{}, false
	}
	if port != "" {
		if h, ok := matchJumpHost(hosts, name, port); ok {
			return h, true
		}
	}
	return matchJumpHost(hosts, name, "")
}

// matchJumpHost finds a saved host by alias, then by hostname. An empty port
// matches any record.
func matchJumpHost(hosts []SSHHost, name, port string) (SSHHost, bool) {
	for _, h := range hosts {
		if h.Alias != "" && strings.EqualFold(strings.TrimSpace(h.Alias), name) && jumpPortMatches(h, port) {
			return h, true
		}
	}
	for _, h := range hosts {
		if strings.EqualFold(strings.TrimSpace(h.Host), name) && jumpPortMatches(h, port) {
			return h, true
		}
	}
	return SSHHost{}, false
}

func jumpPortMatches(h SSHHost, port string) bool {
	if port == "" {
		return true
	}
	saved := strings.TrimSpace(h.Port)
	if saved == "" {
		saved = "22" // an empty saved port means the default, as it does at dial time
	}
	return saved == port
}

// jumpElementNameAndPort splits one ProxyJump element into the host name to
// match on and the port it named, if any, dropping the user@ prefix.
func jumpElementNameAndPort(elem string) (name, port string) {
	s := strings.TrimSpace(elem)
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	switch {
	case strings.HasPrefix(s, "["): // [::1] or [::1]:2222 — bracketed IPv6
		if end := strings.Index(s, "]"); end > 0 {
			rest := s[end+1:]
			return s[1:end], strings.TrimPrefix(rest, ":")
		}
	case strings.Count(s, ":") == 1: // host:port
		i := strings.Index(s, ":")
		return s[:i], s[i+1:]
	}
	// Anything else with several colons is a bare IPv6 literal, which cannot
	// carry a port without brackets — so none of it is a port.
	return s, ""
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
