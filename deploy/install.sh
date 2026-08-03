#!/bin/sh
set -eu

BINARY=${1:-./s12ryt-ipv6}
DATA_DIR=${DATA_DIR:-/etc/s12ryt-ipv6}
UNIT_SOURCE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/systemd/s12ryt-ipv6.service

if [ "$(id -u)" -ne 0 ]; then
    echo "install.sh must run as root" >&2
    exit 1
fi
if [ ! -f "$BINARY" ]; then
    echo "binary not found: $BINARY" >&2
    exit 1
fi

install -d -m 0700 "$DATA_DIR"
install -m 0755 "$BINARY" /usr/local/bin/s12ryt-ipv6
install -m 0644 "$UNIT_SOURCE" /etc/systemd/system/s12ryt-ipv6.service
systemctl daemon-reload
systemctl enable --now s12ryt-ipv6.service
