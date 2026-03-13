#!/usr/bin/env bash

set -euo pipefail

usage() {
  echo "usage: scripts/release.sh vX.Y.Z" >&2
}

die() {
  echo "release: $*" >&2
  exit 1
}

if [ "$#" -ne 1 ]; then
  usage
  exit 1
fi

tag="$1"
case "$tag" in
  v[0-9]*.[0-9]*.[0-9]*)
    ;;
  *)
    usage
    die "tag must look like vX.Y.Z"
    ;;
esac

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || die "not inside a git repository"
cd "$repo_root"

if ! git diff --quiet || ! git diff --cached --quiet; then
  die "working tree must be clean before releasing"
fi

branch="$(git branch --show-current)"
if [ -z "$branch" ]; then
  die "detached HEAD is not supported for release"
fi

git remote get-url origin >/dev/null 2>&1 || die "remote 'origin' is required"

if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  die "local tag $tag already exists"
fi

if git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null 2>&1; then
  die "remote tag $tag already exists"
fi

TAG="$tag" perl -0pi -e 's/const Version = "[^"]+"/const Version = "$ENV{TAG}"/' internal/app/version.go
TAG="$tag" perl -0pi -e 's/^(\s+version:\s+).*$/$1$ENV{TAG}/m' skills/ai-photos/SKILL.md

go test ./...

git add internal/app/version.go skills/ai-photos/SKILL.md
git commit -m "chore(release): $tag"
git tag -a "$tag" -m "Release $tag"
git push origin "$branch"
git push origin "$tag"

echo "released $tag on branch $branch"
