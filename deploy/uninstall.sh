#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "uninstall.sh must run as root" >&2
    exit 1
fi

systemctl disable --now s12ryt-ipv6.service 2>/dev/null || true
rm -f /etc/systemd/system/s12ryt-ipv6.service
systemctl daemon-reload
rm -f /usr/local/bin/s12ryt-ipv6

echo "State in /etc/s12ryt-ipv6 was preserved. Remove it manually after backup if no longer needed."
