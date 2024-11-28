#!/bin/sh
set -e

SCRIPT=$(readlink -f "$0")
# Absolute path this script is in, thus /home/user/bin
SCRIPTPATH=$(dirname "$SCRIPT")

BINARY="${BINARY:-$SCRIPTPATH/../acbrun}"

ALPINE_VERSION="3.20.3"
ALPINE="$SCRIPTPATH/../sample-images/alpine-$ALPINE_VERSION.tar.gz"
ALPINE_SHA256="c0d141e28aea48a56c28650de3ceef70767e3d14da5e6d13f4cc68489e97a3e8"

which acbgrep >/dev/null || (echo "acbgrep is not installed" && exit 1)

"$BINARY" "$ALPINE" "$ALPINE_SHA256" 'cat /etc/alpine-release' | acbgrep "$ALPINE_VERSION"
