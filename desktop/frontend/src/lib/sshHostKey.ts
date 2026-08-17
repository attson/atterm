// Reading a Go *HostKeyUnknownError off a rejected Wails call.
//
// Wails surfaces a rejected Go error as its Error() string, and
// HostKeyUnknownError.Error() returns the bare sentinel "ssh_host_key_unknown"
// — useless on screen and useless to branch on. The fields it carries are what
// matter, so the frontend detects this case structurally instead.

// HostKeyPrompt is one TOFU question: this fingerprint, on this machine.
//
// host is the dial address the Go side handed to x/crypto's HostKeyCallback
// verbatim (net.JoinHostPort(host, port), so always host:port — "10.0.0.9:22",
// or "[::1]:2222" for IPv6) — not a known_hosts name looked up from it, not
// knownhosts.Normalize's "[host]:port" form, and not necessarily the address the
// user typed (an alias resolves to the saved host's own host/port). It
// exists to be echoed back untouched — see AcceptedHostKey
// (desktop/ssh_host.go): an acceptance is scoped to exactly the (host,
// fingerprint) pair the user was shown, and the failed attempt and the retry
// both dial the same address, so both hand the callback the identical
// string. Rebuilding or normalising this value instead of passing it through
// would silently break that match — the retry would present a string that
// no longer equals what the user was shown — with every existing test still
// green, because none of them compare against a normalised form.
//
// hopIndex/hopName say *which machine* the fingerprint belongs to when the
// connection runs through a jump-host chain. hopIndex is 1-based in dial order
// with the destination last; 0 means a direct connection with no chain to
// disambiguate, and a prompt for it must read exactly as it did before jump
// hosts existed.
export interface HostKeyPrompt {
  fingerprint: string;
  host: string;
  hopIndex: number;
  hopName: string;
}

// parseHostKeyPrompt returns the TOFU question a rejection carries, or null if
// it is an ordinary error.
//
// Both halves are required. An error with a fingerprint but no host cannot be
// answered at all: the acceptance the retry would send names no machine, so it
// matches nothing and the same question comes straight back. Treating that as a
// plain error puts the failure where the user can see it instead of behind a
// button that silently does nothing.
export function parseHostKeyPrompt(err: unknown): HostKeyPrompt | null {
  if (!err || typeof err !== "object") return null;
  const e = err as Record<string, unknown>;
  const fingerprint = typeof e.Fingerprint === "string" ? e.Fingerprint : "";
  const host = typeof e.Host === "string" ? e.Host : "";
  if (!fingerprint || !host) return null;
  // Only hopIndex decides whether this is a chain. HopName is populated on the
  // Go side even for a direct connection (ssh_host.go sets it from the saved
  // host whenever there is one), so keying off the name would render "hop 0"
  // for a plain host.
  const hopIndex = typeof e.HopIndex === "number" && e.HopIndex > 0 ? e.HopIndex : 0;
  const hopName = typeof e.HopName === "string" ? e.HopName : "";
  return { fingerprint, host, hopIndex, hopName };
}
