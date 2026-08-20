cask "atterm" do
  arch arm: "arm64", intel: "amd64"

  version "0.4.20"
  sha256 arm:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
         intel: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

  url "https://github.com/attson/atterm/releases/download/v#{version}/AT-Term-darwin-#{arch}.zip"
  name "AT Term"
  desc "Cross-platform terminal with remote takeover"
  homepage "https://github.com/attson/atterm"

  # AT Term ships its own updater (desktop/updater.go, manual-trigger only).
  # Without this, `brew upgrade` would keep reverting the app to whatever
  # version this cask records, fighting the built-in updater.
  auto_updates true

  app "AT Term.app"

  # macOS paths below are derived from the code, not from the XDG defaults
  # the names suggest:
  #   config  internal/appdir/appdir.go ConfigDir -> os.UserConfigDir(),
  #           which on darwin is ~/Library/Application Support
  #   cache   internal/appdir/appdir.go CacheDir  -> os.UserCacheDir(),
  #           which on darwin is ~/Library/Caches
  #   logs    desktop/logging.go defaultLogFilePath, darwin branch
  #   hook    desktop/feishu/endpoint_file.go, which writes the hook-endpoint
  #           discovery file under ~/.config even on macOS
  #
  # The config dir is enumerated file by file ON PURPOSE. Do NOT "simplify"
  # this into a delete of ~/Library/Application Support/atterm: that same
  # directory also holds users.db (+ -wal/-shm), whose user_account_key_wraps
  # table carries the account_key material (internal/userstore/opaque.go), and
  # keyring-fallback.json, the 0600 fallback used when the OS keychain is
  # unavailable (internal/safekeyring/safekeyring.go). Deleting either loses
  # E2EE access to every sealed session, with no recovery. Keychain entries are
  # left alone for the same reason. `zap` means "take the config too", not
  # "take the credentials too"; clearing credentials stays an explicit,
  # separate act by the user.
  zap trash: [
    "~/Library/Application Support/atterm/config.json",
    "~/Library/Application Support/atterm/host_id",
    "~/Library/Application Support/atterm/recovery.json",
    "~/.config/atterm/hook-endpoint",
    "~/Library/Caches/atterm",
    "~/Library/Logs/AT-Term",
  ]

  # Homebrew does NOT exempt casks from Gatekeeper: it applies
  # com.apple.quarantine on install, deliberately mirroring a browser
  # download, and the --no-quarantine escape hatch was deprecated in Homebrew
  # 5.0.0 and has since been removed. So an unsigned app installed via brew
  # raises the same dialog a downloaded one does, and the user has to clear
  # the flag themselves. A `postflight` that stripped it silently is
  # deliberately NOT used here: bypassing Gatekeeper on someone's machine
  # without them ever agreeing to it is a different act from a user running
  # xattr after reading why.
  caveats <<~EOS
    AT Term is not code-signed or notarized yet, so macOS will refuse to open
    it: "AT Term.app cannot be opened because the developer cannot be
    verified."

    Homebrew does not change that: casks are quarantined on install, just
    like a browser download. To clear the flag for this app only:

      xattr -dr com.apple.quarantine "/Applications/AT Term.app"

    This step disappears once the app ships signed and notarized.
  EOS
end
