#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
TARGET_DIR="${ROOT_DIR}/testvectors"

if [ ! -f "${TARGET_DIR}/testvector01.bit" ]; then
    bash "${SCRIPT_DIR}/download_testvectors.sh"
fi

echo "=== Running RFC 6716 / RFC 8251 Conformance Test Matrix (120 Tests) ==="
export OPUS_RFC6716_TESTVECTORS="$TARGET_DIR"

cd "$ROOT_DIR"
go test -v -timeout 30m -run TestRFC6716_FullConformanceMatrix ./test "$@"
