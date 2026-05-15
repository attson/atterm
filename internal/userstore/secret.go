package userstore

// Secret wraps a credential string so that accidental logging or
// fmt-formatting redacts the value. The plaintext is only available via
// the explicit Expose() method, which makes leak sites grep-able.
//
// Layout: a fixed prefix (e.g. "atk_") is preserved for UI listings; the
// rest is replaced by an ellipsis in every fmt verb.
type Secret struct {
	plain  string
	prefix string
}

// NewSecret captures plaintext with a UI-visible prefix. The prefix must
// be a literal substring of plaintext and is shown as-is in fmt output.
func NewSecret(plain, prefix string) Secret {
	return Secret{plain: plain, prefix: prefix}
}

// Expose returns the plaintext. Call sites should grep for ".Expose()"
// to audit credential flow.
func (s Secret) Expose() string { return s.plain }

// Prefix returns the prefix plus the first 4 chars of the secret body
// for UI listings (e.g. "atk_a1b2"). Total length is len(prefix)+4.
func (s Secret) Prefix() string {
	if len(s.plain) < len(s.prefix)+4 {
		return s.prefix
	}
	return s.plain[:len(s.prefix)+4]
}

// String implements fmt.Stringer for %s and %v verbs.
func (s Secret) String() string { return s.Prefix() + "…" }

// GoString implements fmt.GoStringer for %#v.
func (s Secret) GoString() string { return "userstore.Secret(" + s.Prefix() + "…)" }
