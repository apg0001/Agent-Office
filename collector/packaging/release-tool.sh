# shellcheck shell=bash
# agent-office-release 실행 도우미(#180). source해서 쓴다. go가 있으면 그대로, 없으면 Docker golang:1.24-bookworm.
#   release_tool <마운트할 폴더> <인자...>   (폴더는 컨테이너 안에서도 같은 경로로 보인다)
release_tool() {
  local mount=$1
  shift
  if command -v go >/dev/null 2>&1; then
    (cd "$ROOT/collector/core" && go run ./cmd/agent-office-release "$@")
  else
    docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomod \
      -v "$ROOT/collector:/src/collector:ro" -v "$mount:$mount" -w /src/collector/core golang:1.24-bookworm \
      go run ./cmd/agent-office-release "$@"
  fi
}
