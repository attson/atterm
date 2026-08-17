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

  # Deliberately does NOT remove Keychain entries: those hold account_key
  # material, and losing them means losing E2EE access. `zap` means "take the
  # config too", not "take the credentials too".
  zap trash: [
    "~/.config/atterm",
    "~/Library/Logs/atterm",
  ]
end
