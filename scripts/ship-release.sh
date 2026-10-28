#!/usr/bin/env bash
# Create and push the next patch tag only after release preflight passes.
# Usage: ./scripts/ship-release.sh [--line v0.6]

set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

line=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --line)
      [ "$#" -ge 2 ] || { echo "usage: $0 [--line vX.Y]" >&2; exit 2; }
      line="$2"
      shift 2
      ;;
    *)
      echo "usage: $0 [--line vX.Y]" >&2
      exit 2
      ;;
  esac
done

branch=$(git rev-parse --abbrev-ref HEAD)
[ "$branch" = "main" ] || { echo "[ship-release] must run from main (on $branch)." >&2; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "[ship-release] worktree is dirty; commit everything first." >&2; exit 1; }

git fetch origin main --tags
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || {
  echo "[ship-release] local main is not synchronized with origin/main." >&2
  exit 1
}

if [ -n "$line" ]; then
  case "$line" in
    v[0-9]*.[0-9]*) ;;
    *) echo "[ship-release] --line must look like v0.6." >&2; exit 2 ;;
  esac
  pattern="${line}.*"
else
  latest=$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --merged HEAD --sort=-v:refname | head -1)
  [ -n "$latest" ] || { echo "[ship-release] no reachable SemVer release tag found." >&2; exit 1; }
  line="${latest%.*}"
  pattern="${line}.*"
fi

latest_line=$(git tag --list "$pattern" --merged HEAD --sort=-v:refname | head -1)
[ -n "$latest_line" ] || { echo "[ship-release] no reachable tag for ${line}." >&2; exit 1; }
patch=${latest_line##*.}
next_tag="${line}.$((patch + 1))"

if git rev-parse --verify --quiet "refs/tags/$next_tag" >/dev/null || \
   git ls-remote --exit-code --tags origin "refs/tags/$next_tag" >/dev/null 2>&1; then
  echo "[ship-release] ${next_tag} already exists; refusing to overwrite it." >&2
  exit 1
fi

./scripts/check-web-dist.sh
git diff --check

git tag -a "$next_tag" -m "Release $next_tag"
git push origin "$next_tag"
echo "[ship-release] pushed $next_tag"
