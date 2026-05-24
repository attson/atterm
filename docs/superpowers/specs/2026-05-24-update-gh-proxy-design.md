# Update GitHub Proxy Design

## Goal

Let desktop users configure a GitHub release download proxy for auto-update assets, so large downloads can be accelerated in restricted or slow networks.

## Scope

- Add a persisted desktop setting `update_gh_proxy_url`.
- Expose the setting in Settings > Updates.
- Apply the proxy only to GitHub release download files: the platform archive, `SHA256SUMS`, and `SHA256SUMS.sig`.
- Keep update checks against the GitHub API/latest redirect unchanged.
- Keep Ed25519 signature verification and SHA256 checks unchanged and fail-closed.

## URL Behavior

When proxy is empty, downloads use the release URLs as-is.

When proxy is set, only URLs whose host is `github.com` and whose path contains `/releases/download/` are rewritten. The proxy URL is treated as a prefix and the original GitHub URL is appended. For example:

- proxy: `https://gh-proxy.example.com/`
- original: `https://github.com/attson/atterm/releases/download/v0.2.13/AT-Term-windows-amd64.zip`
- download: `https://gh-proxy.example.com/https://github.com/attson/atterm/releases/download/v0.2.13/AT-Term-windows-amd64.zip`

Non-GitHub URLs and non-release-download GitHub URLs are not rewritten.

## Settings UI

Settings > Updates adds an optional text input labeled `GitHub download proxy`. The value is saved independently of auto-check updates. The UI describes that it affects release file downloads only.

## Testing

- Unit-test URL rewriting, including empty proxy, trailing slash normalization, GitHub release downloads, and non-GitHub URLs.
- Unit-test that checksum fetches use the same proxy rewrite path as archives.
- Frontend type-check/build verifies the new Wails bindings and Settings UI compile.
