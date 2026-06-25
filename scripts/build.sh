#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
VERSION="${VERSION:-dev}"

mkdir -p "${DIST_DIR}"
cd "${ROOT_DIR}"

gofmt -w ./cmd/goto ./internal/app
go mod tidy
go test ./...

build() {
  local goos="$1"
  local goarch="$2"
  local ext=""
  if [[ "${goos}" == "windows" ]]; then
    ext=".exe"
  fi

  local output="${DIST_DIR}/goto-${VERSION}-${goos}-${goarch}${ext}"
  echo "building ${output}"
  GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o "${output}" ./cmd/goto
}

build darwin arm64
build darwin amd64
build linux amd64
build linux arm64
build windows amd64
