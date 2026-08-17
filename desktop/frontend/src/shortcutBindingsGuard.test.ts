import { describe, it, expect } from "vitest";

// Repo-wide guard for Task 3's invariant: desktop/config.go's
// migrateShortcutBindings unconditionally clears the legacy
// Plugins.Shortcuts.Bindings slot on every config load, so any .vue
// component that still reads `shortcuts.bindings` / `shortcuts?.bindings`
// off the plugin config (the usePluginConfigStore idiom) will silently see
// an empty map forever, even for a user with real custom bindings — the
// reader and the storage disagree, permanently, with no error anywhere.
//
// This already happened once: ShortcutHints.vue read the legacy slot and
// wasn't caught until a manual review, because the first version of this
// guard only imported and checked four specific files someone had to
// remember to list. That protects the past, not the future. Using
// import.meta.glob to eagerly pull the raw source of every .vue file under
// src/ means a new component added later that copies the old idiom is
// covered automatically — no one has to remember to register it here.
const vueSources = import.meta.glob("./**/*.vue", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const legacyReadPattern = /shortcuts\??\.bindings/;

describe("shortcut bindings: no .vue component reads the legacy plugin-config slot", () => {
  const entries = Object.entries(vueSources);

  // Sanity check on the glob itself: if this ever returns zero files (e.g.
  // the glob pattern or query option stops matching after a Vite upgrade),
  // every per-file check below would vacuously pass and the guard would be
  // silently defeated. Pin it to files we know exist today.
  it("found .vue sources to scan (glob sanity check)", () => {
    const paths = entries.map(([p]) => p);
    expect(paths.length).toBeGreaterThan(50);
    expect(paths.some((p) => p.endsWith("/App.vue"))).toBe(true);
    expect(paths.some((p) => p.endsWith("/components/ShortcutHints.vue"))).toBe(true);
    expect(paths.some((p) => p.endsWith("/components/SettingsShortcuts.vue"))).toBe(true);
  });

  for (const [path, source] of entries) {
    it(`${path} does not read shortcut bindings off the plugin config`, () => {
      expect(
        source,
        `${path} matched the legacy \`shortcuts.bindings\` read pattern. ` +
          "Shortcut bindings must come from getShortcutBindings() (or a prop " +
          "fed by it, as ShortcutHints.vue does from App.vue), not " +
          "usePluginConfigStore's shortcuts sub-key — that slot is cleared on " +
          "every config load and will read back empty."
      ).not.toMatch(legacyReadPattern);
    });
  }

  // The read-side check above is deliberately repo-wide because any file
  // could plausibly grow a reader. The write side is narrower on purpose:
  // SetPluginConfig is legitimately called by several unrelated settings
  // panels (file explorer, translate, plugin enablement) for their own
  // sub-keys, so a blanket "no file may call SetPluginConfig" would false
  // positive on all of them. SettingsShortcuts.vue specifically must not,
  // since it is the one component whose job is writing shortcut bindings.
  it("SettingsShortcuts.vue does not write shortcut bindings through SetPluginConfig", () => {
    const match = entries.find(([p]) => p.endsWith("/components/SettingsShortcuts.vue"));
    expect(match, "SettingsShortcuts.vue not found by the glob").toBeTruthy();
    const [, settingsShortcutsSource] = match!;
    // Matched as a call (trailing paren) rather than a bare identifier: the
    // component legitimately mentions "SetPluginConfig" by name in a comment
    // explaining why it isn't called.
    expect(settingsShortcutsSource).not.toMatch(/SetPluginConfig\s*\(/);
  });
});
