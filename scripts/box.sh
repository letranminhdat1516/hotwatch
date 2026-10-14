#!/usr/bin/env bash
# Run a command on agent-box inside a throwaway golang:1.27 container.
# usage: scripts/box.sh [--docker] '<command>'
set -euo pipefail

docker_sock=()
if [ "${1:-}" = "--docker" ]; then
  docker_sock=(-v /var/run/docker.sock:/var/run/docker.sock)
  shift
fi
[ $# -eq 1 ] || { echo "usage: scripts/box.sh [--docker] '<command>'" >&2; exit 64; }

root="$(git rev-parse --show-toplevel)"
"$HOME/.claude/skills/cicca/scripts/sync.sh" "$root" >/dev/null

remote="docker run --rm -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false"
remote+=" -v ~/src/hotwatch:/src -v hotwatch-gomod:/go/pkg/mod -v hotwatch-gocache:/root/.cache/go-build"
for a in ${docker_sock[@]+"${docker_sock[@]}"}; do remote+=" $(printf '%q' "$a")"; done
remote+=" -w /src golang:1.27 bash -c $(printf '%q' "$1")"

exec ssh agent-box "$remote"
