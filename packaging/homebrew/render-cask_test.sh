#!/usr/bin/env bash
# packaging/homebrew/render-cask_test.sh — tests for render-cask.sh.
#
# No existing bash-test convention was found under scripts/ or
# .github/scripts/: those directories hold build/package scripts (dmg
# packaging, deb packaging, checksum signing, a grep-based isolation check),
# not a test harness with pass/fail assertions. This follows the plainest
# thing that works: render fixtures under testdata/, diff the happy path
# against a known-good expected.rb, and assert exit code + stderr content
# for the failure paths.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
render="$script_dir/render-cask.sh"
testdata="$script_dir/testdata"

pass=0
fail=0

ok() {
  pass=$((pass + 1))
  echo "ok - $1"
}

bad() {
  fail=$((fail + 1))
  echo "FAIL - $1"
}

# --- Test 1: happy path. version loses its v prefix, and each arch's sha
# lands on that arch's own line — not the other one's. A byte-for-byte diff
# against expected.rb catches a swap: if arm and amd64 shas were transposed,
# the rendered sha256 block would no longer match expected.rb at all (the
# fixture uses two distinguishable values, all-'a's vs all-'b's, precisely
# so a swap cannot accidentally still diff clean). ---
actual="$(mktemp)"
diff_out="$(mktemp)"
if "$render" v0.4.20 "$testdata/SHA256SUMS.ok" >"$actual"; then
  if diff -u "$testdata/expected.rb" "$actual" >"$diff_out" 2>&1; then
    ok "happy path matches expected.rb byte-for-byte (version unprefixed, arch shas not swapped)"
  else
    bad "happy path output differs from expected.rb"
    cat "$diff_out"
  fi
else
  bad "happy path: render-cask.sh exited non-zero"
fi
rm -f "$actual" "$diff_out"

# --- Test 2: SHA256SUMS missing the arm64 zip line must fail loudly and
# name arm64, not silently render a cask with an empty sha256 (that cask
# would be syntactically fine and only fail later, at the user's
# `brew install`). ---
stderr="$(mktemp)"
if "$render" v0.4.20 "$testdata/SHA256SUMS.missing-arm64" >/dev/null 2>"$stderr"; then
  bad "missing-arm64: expected non-zero exit, got success"
elif grep -qi "arm64" "$stderr"; then
  ok "missing-arm64: fails non-zero and names arm64 on stderr"
else
  bad "missing-arm64: failed but stderr didn't name arm64 (was: $(cat "$stderr"))"
fi
rm -f "$stderr"

# --- Test 3: symmetric case for amd64. ---
stderr="$(mktemp)"
if "$render" v0.4.20 "$testdata/SHA256SUMS.missing-amd64" >/dev/null 2>"$stderr"; then
  bad "missing-amd64: expected non-zero exit, got success"
elif grep -qi "amd64" "$stderr"; then
  ok "missing-amd64: fails non-zero and names amd64 on stderr"
else
  bad "missing-amd64: failed but stderr didn't name amd64 (was: $(cat "$stderr"))"
fi
rm -f "$stderr"

# --- Test 4: a version that is not a plain vN.N.N must be rejected before
# it reaches sed. The substitution is unquoted against sed's replacement
# metacharacters (/ and &), so a version carrying one would render a
# syntactically valid cask with a broken url — a failure that would only
# surface at `brew install` time, on a user's machine. ---
for badver in "v0.4/20" "0.4.20&evil" "latest" ""; do
  stderr="$(mktemp)"
  if "$render" "$badver" "$testdata/SHA256SUMS.ok" >/dev/null 2>"$stderr"; then
    bad "bad version '$badver': expected non-zero exit, got success"
  elif grep -qi "version must look like" "$stderr"; then
    ok "bad version '$badver': rejected before rendering"
  else
    bad "bad version '$badver': failed but stderr didn't explain why (was: $(cat "$stderr"))"
  fi
  rm -f "$stderr"
done

# --- Test 5: the guard must not reject the shapes we actually ship. ---
for goodver in "v0.4.20" "0.4.20" "v1.10.3-rc.1"; do
  if "$render" "$goodver" "$testdata/SHA256SUMS.ok" >/dev/null 2>&1; then
    ok "good version '$goodver': accepted"
  else
    bad "good version '$goodver': rejected by the version guard"
  fi
done

echo "----"
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
