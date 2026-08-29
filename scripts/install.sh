#!/bin/sh
# lb-98h
# Build and install lb. Prefers make; falls back to go install.
set -eu
cd "$(dirname "$0")/.."
if command -v make >/dev/null 2>&1; then
	PREFIX="${PREFIX:-${HOME}/.local}"
	export PREFIX
	exec make install
fi
exec go install ./cmd/lb
