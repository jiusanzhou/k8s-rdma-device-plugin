#!/usr/bin/env bash
set -euo pipefail

BINARY="bin/k8s-rdma-device-plugin"
MODULE="github.com/jiusanzhou/k8s-rdma-device-plugin"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

echo "Building ${BINARY} version=${VERSION} commit=${COMMIT}"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-s -w -X ${MODULE}/cmd/k8s-rdma-device-plugin/app.version=${VERSION}" \
    -o "${BINARY}" \
    -gcflags="all=-N -l" \
    ./cmd/k8s-rdma-device-plugin

echo "Done: ${BINARY}"
