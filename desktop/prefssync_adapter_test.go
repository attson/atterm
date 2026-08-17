package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/prefssync"
)

func newTestConfigStore(t *testing.T) *configStore {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// os.UserConfigDir consults XDG_CONFIG_HOME on Linux (and other
	// platform-specific vars) BEFORE falling back to $HOME/.config. CI runners
	// set XDG_CONFIG_HOME, so without clearing it every test's configStore
	// would share one config.json and cross-contaminate (host/key rows leaking
	// between tests). Point every config-dir env at the per-test temp dir so
	// each configStore is hermetic.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("LocalAppData", filepath.Join(tmp, "local")) // Windows
	return loadConfig()
}

func TestAdapter_PinnedSessionIds_RoundTrip(t *testing.T) {
	cs := newTestConfigStore(t)
	a := newAppConfigAdapter(cs, func() []byte { return nil })

	want := []string{"sid-a", "sid-b"}
	raw, _ := json.Marshal(want)
	if err := a.WriteValue("pinned_session_ids", raw); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}

	got, ok := a.ReadValue("pinned_session_ids")
	if !ok {
		t.Fatal("ReadValue returned ok=false")
	}
	var back []string
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back) != 2 || back[0] != "sid-a" || back[1] != "sid-b" {
		t.Fatalf("got %v; want %v", back, want)
	}

	// Adapter must expose the new key.
	found := false
	for _, k := range a.Keys() {
		if k == "pinned_session_ids" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Keys() missing pinned_session_ids: %v", a.Keys())
	}

	_ = prefssync.Meta{} // silence unused import if none other
}

// TestL1KeysAreWhitelisted checks whitelist membership only — the actual
// read/write behavior of each key is exercised by TestAdapterRoundTripsL1Keys
// and TestAdapterCursorBlinkThreeStates below.
func TestL1KeysAreWhitelisted(t *testing.T) {
	keys := []string{
		"terminal_theme", "terminal_font_head", "terminal_font_size",
		"terminal_line_height", "terminal_cursor_style", "terminal_cursor_blink",
		"terminal_scrollback", "default_shell", "shortcut_bindings",
	}
	for _, k := range keys {
		if !slices.Contains(prefssync.SyncedKeys(), k) {
			t.Errorf("%s is not in SyncedKeys()", k)
		}
	}
}

// TestAdapterRoundTripsL1Keys drives ReadValue on a source adapter and
// WriteValue on a *separate, fresh* adapter for each of the eight scalar/map
// L1 keys (terminal_cursor_blink's nil/false/true states get their own test
// below, since marshalPtr's nil-handling doesn't fit this shape). Two
// adapters — rather than round-tripping through the same one — are what
// actually proves ReadValue and WriteValue agree on wire format instead of
// both happening to no-op on the same struct.
//
// Each case also checks ReadValue on an *untouched* appConfig against
// wantZero. This is the raw-field rule's guard: prefssync must sync what the
// user explicitly set, never a resolved *OrDefault() value. A machine that
// never touched, say, font size must report the JSON zero value (0), not the
// resolved default (13) — otherwise a fresh machine would push its default
// up as if it were a deliberate choice and clobber a real value set
// elsewhere. Swap any case's ReadValue to call the *OrDefault() accessor and
// wantZero must fail loudly.
func TestAdapterRoundTripsL1Keys(t *testing.T) {
	cases := []struct {
		key      string
		wantZero string // ReadValue on a never-touched appConfig
		seed     func(cfg *appConfig)
		verify   func(t *testing.T, cfg appConfig)
	}{
		{
			key:      "terminal_theme",
			wantZero: `""`,
			seed:     func(cfg *appConfig) { cfg.TerminalTheme = "nord" },
			verify: func(t *testing.T, cfg appConfig) {
				if cfg.TerminalTheme != "nord" {
					t.Fatalf("TerminalTheme = %q; want nord", cfg.TerminalTheme)
				}
			},
		},
		{
			key:      "terminal_font_head",
			wantZero: `""`,
			seed:     func(cfg *appConfig) { cfg.TerminalFontHead = "JetBrains Mono" },
			verify: func(t *testing.T, cfg appConfig) {
				if cfg.TerminalFontHead != "JetBrains Mono" {
					t.Fatalf("TerminalFontHead = %q; want JetBrains Mono", cfg.TerminalFontHead)
				}
			},
		},
		{
			// The sharpest case: default is 13, so a swap to
			// TerminalFontSizeOrDefault() in ReadValue fails right here.
			key:      "terminal_font_size",
			wantZero: `0`,
			seed:     func(cfg *appConfig) { cfg.TerminalFontSize = 16 },
			verify: func(t *testing.T, cfg appConfig) {
				if cfg.TerminalFontSize != 16 {
					t.Fatalf("TerminalFontSize = %d; want 16", cfg.TerminalFontSize)
				}
			},
		},
		{
			key:      "terminal_line_height",
			wantZero: `0`,
			seed:     func(cfg *appConfig) { cfg.TerminalLineHeight = 1.2 },
			verify: func(t *testing.T, cfg appConfig) {
				if cfg.TerminalLineHeight != 1.2 {
					t.Fatalf("TerminalLineHeight = %v; want 1.2", cfg.TerminalLineHeight)
				}
			},
		},
		{
			key:      "terminal_cursor_style",
			wantZero: `""`,
			seed:     func(cfg *appConfig) { cfg.TerminalCursorStyle = "bar" },
			verify: func(t *testing.T, cfg appConfig) {
				if cfg.TerminalCursorStyle != "bar" {
					t.Fatalf("TerminalCursorStyle = %q; want bar", cfg.TerminalCursorStyle)
				}
			},
		},
		{
			key:      "terminal_scrollback",
			wantZero: `0`,
			seed:     func(cfg *appConfig) { cfg.TerminalScrollback = 9000 },
			verify: func(t *testing.T, cfg appConfig) {
				if cfg.TerminalScrollback != 9000 {
					t.Fatalf("TerminalScrollback = %d; want 9000", cfg.TerminalScrollback)
				}
			},
		},
		{
			key:      "default_shell",
			wantZero: `""`,
			seed:     func(cfg *appConfig) { cfg.DefaultShell = "/bin/zsh" },
			verify: func(t *testing.T, cfg appConfig) {
				if cfg.DefaultShell != "/bin/zsh" {
					t.Fatalf("DefaultShell = %q; want /bin/zsh", cfg.DefaultShell)
				}
			},
		},
		{
			key:      "shortcut_bindings",
			wantZero: `null`,
			seed: func(cfg *appConfig) {
				cfg.ShortcutBindings = map[string]string{"tab.new": "Mod+KeyP"}
			},
			verify: func(t *testing.T, cfg appConfig) {
				if cfg.ShortcutBindings["tab.new"] != "Mod+KeyP" || len(cfg.ShortcutBindings) != 1 {
					t.Fatalf("ShortcutBindings = %v; want {tab.new: Mod+KeyP}", cfg.ShortcutBindings)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			// Untouched config: ReadValue must report the raw zero value.
			zeroStore := newTestConfigStore(t)
			zeroAdapter := newAppConfigAdapter(zeroStore, func() []byte { return nil })
			zeroRaw, ok := zeroAdapter.ReadValue(c.key)
			if !ok {
				t.Fatalf("ReadValue(%s) on untouched config: ok=false", c.key)
			}
			if string(zeroRaw) != c.wantZero {
				t.Fatalf("ReadValue(%s) on untouched config = %s; want %s (a resolved *OrDefault() value would leak through here)", c.key, zeroRaw, c.wantZero)
			}

			// Source: seed a distinctive value and read it back as wire JSON.
			src := newTestConfigStore(t)
			srcAdapter := newAppConfigAdapter(src, func() []byte { return nil })
			cfg := src.Get()
			c.seed(&cfg)
			if err := src.Set(cfg); err != nil {
				t.Fatalf("seed config: %v", err)
			}
			raw, ok := srcAdapter.ReadValue(c.key)
			if !ok {
				t.Fatalf("ReadValue(%s) ok=false", c.key)
			}

			// Destination: a brand-new adapter, never touched. WriteValue must
			// land the same value there — proving Read/Write agree on the wire
			// format rather than both happening to no-op against one struct.
			dst := newTestConfigStore(t)
			dstAdapter := newAppConfigAdapter(dst, func() []byte { return nil })
			if err := dstAdapter.WriteValue(c.key, raw); err != nil {
				t.Fatalf("WriteValue(%s, %s): %v", c.key, raw, err)
			}
			c.verify(t, dst.Get())
		})
	}
}

// TestAdapterCursorBlinkThreeStates pins the *bool round trip: never-set
// (nil), explicit false, and explicit true must all survive distinctly. This
// is the one case in the adapter that is already more careful than its
// neighbors — WriteValue unmarshals straight into *bool instead of the `var
// b bool` pattern the other boolean-shaped keys use elsewhere in this file,
// which matters here because `var b bool` would coerce a JSON null into
// false and erase the "never set" state. A later "consistency" refactor
// toward that pattern must break this test.
func TestAdapterCursorBlinkThreeStates(t *testing.T) {
	trueVal, falseVal := true, false
	cases := []struct {
		name string
		seed *bool
	}{
		{"never_set", nil},
		{"explicit_false", &falseVal},
		{"explicit_true", &trueVal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := newTestConfigStore(t)
			srcAdapter := newAppConfigAdapter(src, func() []byte { return nil })
			cfg := src.Get()
			cfg.TerminalCursorBlink = c.seed
			if err := src.Set(cfg); err != nil {
				t.Fatalf("seed config: %v", err)
			}

			raw, ok := srcAdapter.ReadValue("terminal_cursor_blink")
			if c.seed == nil {
				if ok {
					t.Fatalf("ReadValue ok=true for a never-set blink; nil must not sync as a value")
				}
				return
			}
			if !ok {
				t.Fatal("ReadValue ok=false for an explicitly-set blink")
			}

			dst := newTestConfigStore(t)
			dstAdapter := newAppConfigAdapter(dst, func() []byte { return nil })
			if err := dstAdapter.WriteValue("terminal_cursor_blink", raw); err != nil {
				t.Fatalf("WriteValue: %v", err)
			}
			got := dst.Get().TerminalCursorBlink
			if got == nil {
				t.Fatalf("TerminalCursorBlink after round trip = nil; want %v", *c.seed)
			}
			if *got != *c.seed {
				t.Fatalf("TerminalCursorBlink after round trip = %v; want %v", *got, *c.seed)
			}
		})
	}
}

func TestIsPrefCustomizedCoversL1Keys(t *testing.T) {
	blink := false
	customized := appConfig{
		TerminalTheme:       "nord",
		TerminalFontHead:    "JetBrains Mono",
		TerminalFontSize:    16,
		TerminalLineHeight:  1.2,
		TerminalCursorStyle: "bar",
		TerminalCursorBlink: &blink,
		TerminalScrollback:  9000,
		DefaultShell:        "/bin/zsh",
		ShortcutBindings:    map[string]string{"tab.new": "Mod+KeyP"},
	}
	isCustom := isPrefCustomized(customized)
	isVirgin := isPrefCustomized(appConfig{})
	for _, k := range []string{
		"terminal_theme", "terminal_font_head", "terminal_font_size",
		"terminal_line_height", "terminal_cursor_style", "terminal_cursor_blink",
		"terminal_scrollback", "default_shell", "shortcut_bindings",
	} {
		if !isCustom(k) {
			t.Errorf("%s: explicitly set value must count as customized — otherwise first login pulls a remote value over it", k)
		}
		if isVirgin(k) {
			t.Errorf("%s: an untouched config must not count as customized", k)
		}
	}
}

// TestIsPrefCustomizedProfiles mirrors TestIsPrefCustomizedCoversL1Keys for
// the profiles_encrypted key (design §3 registration point #4): a profile
// list means "customized", an empty one does not.
func TestIsPrefCustomizedProfiles(t *testing.T) {
	isCustom := isPrefCustomized(appConfig{Profiles: []SessionProfile{{ID: "a", Name: "Work"}}})
	if !isCustom("profiles_encrypted") {
		t.Error("a non-empty profile list must count as customized")
	}
	isVirgin := isPrefCustomized(appConfig{})
	if isVirgin("profiles_encrypted") {
		t.Error("an untouched (no profiles) config must not count as customized")
	}
}

// TestAdapterProfilesEncryptedNoAccountKeySkips mirrors
// TestAdapterSSHHostsNoAccountKeySkips: without an account key,
// profiles_encrypted must behave as "local only" on both sides — ReadValue
// reports nothing to sync, WriteValue silently ignores an inbound blob it
// has no key to open.
func TestAdapterProfilesEncryptedNoAccountKeySkips(t *testing.T) {
	cs := newTestConfigStore(t)
	a := newAppConfigAdapter(cs, func() []byte { return nil })

	if _, ok := a.ReadValue("profiles_encrypted"); ok {
		t.Fatal("no account key must skip ReadValue (ok=false)")
	}
	if err := a.WriteValue("profiles_encrypted", json.RawMessage(`"x"`)); err != nil {
		t.Fatalf("WriteValue no-op expected, got %v", err)
	}
}

// TestAdapterProfilesEncryptedRoundTrip drives ReadValue on a source adapter
// and WriteValue on a fresh destination adapter, the same two-adapter shape
// TestAdapterSSHHostsEncryptedRoundTrip uses to prove the wire format
// actually agrees between the two sides rather than both happening to no-op
// on the same struct.
func TestAdapterProfilesEncryptedRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	accountKey := func() []byte { return key }

	src := newTestConfigStore(t)
	cfg := src.Get()
	cfg.Profiles = []SessionProfile{
		{ID: "a", Name: "Work", Shell: "/bin/zsh", SyncEnv: true, Env: map[string]string{"K": "V"}},
	}
	cfg.DefaultProfileID = "a"
	if err := src.Set(cfg); err != nil {
		t.Fatal(err)
	}
	srcAdapter := newAppConfigAdapter(src, accountKey)

	val, ok := srcAdapter.ReadValue("profiles_encrypted")
	if !ok {
		t.Fatal("expected ReadValue ok with account key")
	}
	if strings.Contains(string(val), "Work") {
		t.Fatalf("sealed value must not contain plaintext profile data: %s", val)
	}

	dst := newTestConfigStore(t)
	dstAdapter := newAppConfigAdapter(dst, accountKey)
	if err := dstAdapter.WriteValue("profiles_encrypted", val); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}
	got := dst.Get()
	if len(got.Profiles) != 1 || got.Profiles[0].ID != "a" || got.Profiles[0].Name != "Work" {
		t.Fatalf("profiles not restored: %+v", got.Profiles)
	}
	if got.Profiles[0].Env["K"] != "V" {
		t.Fatalf("synced env not restored: %+v", got.Profiles[0].Env)
	}
	if got.DefaultProfileID != "a" {
		t.Fatalf("default profile id not restored: %q", got.DefaultProfileID)
	}
}

// TestAdapterProfilesEncryptedWriteValueMergesNotReplaces is the trap this
// whole task exists to close: WriteValue must merge an inbound payload with
// whatever profiles already exist locally, not replace them wholesale. A
// profile with a machine-local (unsynced, SyncEnv=false) env must survive a
// pull whose payload — by design, since that env never left the source
// machine — carries no env for that profile id at all.
//
// This test is the one the brief asks to prove by mutation: temporarily
// changing WriteValue's profiles_encrypted case from
// `c.Profiles = mergeProfiles(c.Profiles, incoming)` to
// `c.Profiles = incoming` makes it fail (env wiped) instead of pass.
func TestAdapterProfilesEncryptedWriteValueMergesNotReplaces(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	accountKey := func() []byte { return key }

	// dst already has a locally-configured env for profile "a" — never
	// synced (SyncEnv defaults false).
	dst := newTestConfigStore(t)
	cfg := dst.Get()
	cfg.Profiles = []SessionProfile{
		{ID: "a", Name: "Work", Env: map[string]string{"LOCAL_SECRET": "shh"}},
	}
	if err := dst.Set(cfg); err != nil {
		t.Fatal(err)
	}
	dstAdapter := newAppConfigAdapter(dst, accountKey)

	// A payload from another machine that renamed the same profile and
	// never opted into env sync — its sealed blob carries no Env for "a".
	blob, err := sealProfiles(key, []SessionProfile{{ID: "a", Name: "Work (renamed)"}}, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := dstAdapter.WriteValue("profiles_encrypted", blob); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}

	got := dst.Get().Profiles
	if len(got) != 1 {
		t.Fatalf("expected 1 profile, got %d: %+v", len(got), got)
	}
	if got[0].Name != "Work (renamed)" {
		t.Errorf("non-env fields must still take the incoming value, got %q", got[0].Name)
	}
	if got[0].Env["LOCAL_SECRET"] != "shh" {
		t.Errorf("WriteValue replaced instead of merged: local-only env was wiped by a pull that carried none, got %+v", got[0].Env)
	}
}

// TestAdapterProfilesEncryptedWriteValueDropsMalformedEntries is the carried
// Task 2 review finding (b): a malformed inbound payload must not corrupt
// the whole profile list. Chose "drop the bad entries, keep the rest" (see
// filterValidProfiles) over "reject the whole payload" — the latter is
// exactly the failure shape that made item 21's relay whitelist bug so
// damaging (one bad entry blocking everything else from syncing).
func TestAdapterProfilesEncryptedWriteValueDropsMalformedEntries(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	accountKey := func() []byte { return key }

	dst := newTestConfigStore(t)
	dstAdapter := newAppConfigAdapter(dst, accountKey)

	blob, err := sealProfiles(key, []SessionProfile{
		{ID: "a", Name: "Good"},
		{ID: "", Name: "No id"},
		{ID: "b", Name: ""},
		{ID: "a", Name: "Duplicate of a"},
	}, "b" /* dangling: "b" got dropped for having no name */)
	if err != nil {
		t.Fatal(err)
	}

	if err := dstAdapter.WriteValue("profiles_encrypted", blob); err != nil {
		t.Fatalf("WriteValue must not reject the whole payload for one bad entry: %v", err)
	}

	got := dst.Get()
	if len(got.Profiles) != 1 || got.Profiles[0].ID != "a" {
		t.Fatalf("expected only the valid profile to survive, got %+v", got.Profiles)
	}
	if got.DefaultProfileID != "" {
		t.Errorf("default profile id referencing a dropped entry must not linger, got %q", got.DefaultProfileID)
	}
}
