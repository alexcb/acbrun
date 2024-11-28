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

runc delete --force test2 || true

rm -rf /tmp/test2 || true
"$BINARY" --verbose --reentrant --name test2 "$ALPINE" "$ALPINE_SHA256" "echo foo" | acbgrep foo

#acbtest -d /tmp/test2
#runc state test2
"$BINARY" --verbose --reentrant --name test2 "$ALPINE" "$ALPINE_SHA256" "echo bar" | acbgrep bar
