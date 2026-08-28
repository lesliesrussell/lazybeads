#!/bin/sh
# lb-17y
# Install the lb binary into GOPATH/bin or GOBIN.
set -eu
cd "$(dirname "$0")/.."
exec go install ./cmd/lb
