#!/bin/sh
set -eu
version=${1:?Usage: install.sh vX.Y.Z}
case "$version" in
    v[0-9]*.[0-9]*.[0-9]*) ;;
    *) printf '%s\n' 'Expected a release tag such as v0.1.0' >&2; exit 1 ;;
esac
case "$version" in *[!A-Za-z0-9._-]*) exit 1 ;; esac
case "$(uname -s)" in
    Darwin) target_os=Darwin ;;
    Linux) target_os=Linux ;;
    *) printf '%s\n' 'Download the Windows archive from GitHub Releases.' >&2; exit 1 ;;
esac
case "$(uname -m)" in
    x86_64|amd64) target_arch=x86_64 ;;
    arm64|aarch64) target_arch=arm64 ;;
    *) printf '%s\n' 'Unsupported architecture' >&2; exit 1 ;;
esac
if [ "$target_os/$target_arch" = Linux/arm64 ]; then
    printf '%s\n' 'Linux arm64: install from source with Go.' >&2
    exit 1
fi
archive="tack_${target_os}_${target_arch}.tar.gz"
base="https://github.com/javiercm1410/tack/releases/download/$version"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$base/$archive" -o "$tmp/$archive"
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$base/checksums.txt" -o "$tmp/checksums.txt"
expected=$(awk -v name="$archive" '$2 == name {print $1}' "$tmp/checksums.txt")
if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
else
    actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
fi
if [ -z "$expected" ] || [ "$actual" != "$expected" ]; then
    printf '%s\n' 'Checksum verification failed' >&2
    exit 1
fi
tar -xzf "$tmp/$archive" -C "$tmp" tack
install_dir=${TACK_INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p "$install_dir"
install -m 0755 "$tmp/tack" "$install_dir/tack"
printf 'Installed tack to %s/tack\n' "$install_dir"
