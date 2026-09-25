#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname -- "$SCRIPT_DIR")"

if [[ $# -ne 1 ]] || [[ ! "$1" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    printf 'Usage: %s vX.Y.Z\n' "$0" >&2
    exit 1
fi
VERSION="$1"

git -C "$ROOT_DIR" fetch origin master

WORKTREE_STATUS="$(git -C "$ROOT_DIR" status --porcelain)"
if [[ -n "$WORKTREE_STATUS" ]]; then
    printf 'Worktree must be clean.\n' >&2
    exit 1
fi

LOCAL_HEAD="$(git -C "$ROOT_DIR" rev-parse HEAD)"
REMOTE_MASTER="$(git -C "$ROOT_DIR" rev-parse origin/master)"
if [[ "$LOCAL_HEAD" != "$REMOTE_MASTER" ]]; then
    printf 'HEAD must match origin/master.\n' >&2
    exit 1
fi

VERSION_FILE_VERSION="$(<"$ROOT_DIR/VERSION")"
if [[ "$VERSION_FILE_VERSION" != "$VERSION" ]]; then
    printf 'VERSION does not match %s.\n' "$VERSION" >&2
    exit 1
fi

if git -C "$ROOT_DIR" show-ref --verify --quiet "refs/tags/$VERSION"; then
    printf 'Local tag already exists: %s\n' "$VERSION" >&2
    exit 1
fi

REMOTE_TAG="$(git -C "$ROOT_DIR" ls-remote --tags origin "refs/tags/$VERSION")"
if [[ -n "$REMOTE_TAG" ]]; then
    printf 'Remote tag already exists: %s\n' "$VERSION" >&2
    exit 1
fi

git -C "$ROOT_DIR" tag --annotate "$VERSION" --message "Release $VERSION" HEAD
git -C "$ROOT_DIR" push origin "refs/tags/$VERSION"
