#!/bin/sh
set -eu
mkdir -p bin
for target in darwin/arm64 darwin/amd64 linux/amd64 windows/amd64; do
    target_os=${target%/*}
    target_arch=${target#*/}
    suffix=
    if [ "$target_os" = windows ]; then suffix=.exe; fi
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
        go build -trimpath -ldflags '-s -w' -o "bin/tack_${target_os}_${target_arch}${suffix}" ./cmd/tack
done
