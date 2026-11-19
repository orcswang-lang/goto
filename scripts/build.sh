#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
VERSION="${VERSION:-dev}"

mkdir -p "${DIST_DIR}"
cd "${ROOT_DIR}"

gofmt -w ./cmd ./internal
go mod tidy
go test ./...

build() {
  local goos="$1"
  local goarch="$2"
  local name="$3"
  local cmd="$4"
  local ext=""
  if [[ "${goos}" == "windows" ]]; then
    ext=".exe"
  fi

  local output="${DIST_DIR}/${name}-${VERSION}-${goos}-${goarch}${ext}"
  echo "building ${output}"
  GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o "${output}" "${cmd}"
}

build darwin arm64 goto ./cmd/goto
build darwin amd64 goto ./cmd/goto
build linux amd64 goto ./cmd/goto
build linux arm64 goto ./cmd/goto
build windows amd64 goto ./cmd/goto

build darwin arm64 goto-mcp ./cmd/goto-mcp
build darwin amd64 goto-mcp ./cmd/goto-mcp
build linux amd64 goto-mcp ./cmd/goto-mcp
build linux arm64 goto-mcp ./cmd/goto-mcp
build windows amd64 goto-mcp ./cmd/goto-mcp
