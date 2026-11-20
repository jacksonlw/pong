#!/bin/bash
# Installs Pong from a downloaded release without Homebrew. Two separate steps, never piped:
#
#   curl -fLO https://github.com/jacksonlw/homebrew-pong/releases/latest/download/Pong-<version>.zip         # and the .sha256 and install.sh
#   bash install.sh Pong-<version>.zip
#
# On the Mac: verifies the zip against Pong-<version>.zip.sha256 beside it, copies Pong.app into
# /Applications (or ~/Applications when /Applications is not writable), and links the pong
# command-line tool into a bin directory. Overridable: PONG_APPDIR, PONG_BINDIR.
#
# On Linux (x86-64 and arm64): pass pong-<version>-linux-<arch>.tar.gz instead. It verifies it
# the same way and copies pong and pong-engine into ~/.local/bin (or PONG_BINDIR); `pong setup`
# then runs the Engine as a systemd user service.
set -euo pipefail
zip=${1:?usage: install.sh Pong-<version>.zip | pong-<version>-linux-<arch>.tar.gz}
sum="$zip.sha256"
[[ -f "$sum" ]] || { echo "install: $sum not found beside the download" >&2; exit 1; }

want=$(awk '{print $1}' "$sum")
if command -v shasum >/dev/null; then
    got=$(shasum -a 256 "$zip" | awk '{print $1}')
else
    got=$(sha256sum "$zip" | awk '{print $1}')
fi
[[ "$want" == "$got" ]] || { echo "install: checksum mismatch for $zip" >&2; exit 1; }
echo "checksum ok"

if [[ "$zip" == *.tar.gz ]]; then
    bindir=${PONG_BINDIR:-"$HOME/.local/bin"}
    mkdir -p "$bindir"
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    tar -C "$tmp" -xzf "$zip"
    dir=$(find "$tmp" -mindepth 1 -maxdepth 1 -type d -name 'pong-*' | head -1)
    [[ -n "$dir" ]] || { echo "install: no pong-* folder in $zip" >&2; exit 1; }
    for cmd in pong pong-engine; do
        [[ -f "$dir/$cmd" ]] || { echo "install: $cmd not in $zip" >&2; exit 1; }
        install -m 0755 "$dir/$cmd" "$bindir/$cmd"
        echo "installed $bindir/$cmd"
    done
    case ":$PATH:" in *":$bindir:"*) ;; *) echo "note: add $bindir to your PATH" ;; esac
    exit 0
fi

appdir=${PONG_APPDIR:-}
if [[ -z "$appdir" ]]; then
    if [[ -w /Applications ]]; then appdir=/Applications; else appdir="$HOME/Applications"; fi
fi
bindir=${PONG_BINDIR:-}
if [[ -z "$bindir" ]]; then
    if [[ -w /opt/homebrew/bin ]]; then bindir=/opt/homebrew/bin
    elif [[ -w /usr/local/bin ]]; then bindir=/usr/local/bin
    else bindir="$HOME/.local/bin"; fi
fi
mkdir -p "$appdir" "$bindir"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
ditto -x -k "$zip" "$tmp"
[[ -d "$tmp/Pong.app" ]] || { echo "install: Pong.app not in $zip" >&2; exit 1; }

rm -rf "$appdir/Pong.app"
ditto "$tmp/Pong.app" "$appdir/Pong.app"
ln -sf "$appdir/Pong.app/Contents/Helpers/pong" "$bindir/pong"
echo "installed $appdir/Pong.app"
echo "linked $bindir/pong"
case ":$PATH:" in *":$bindir:"*) ;; *) echo "note: add $bindir to your PATH" ;; esac
