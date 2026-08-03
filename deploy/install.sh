#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
BINARY=${1:-./s12ryt-ipv6}
DATA_DIR=${DATA_DIR:-/etc/s12ryt-ipv6}
UNIT_SOURCE="$SCRIPT_DIR/systemd/s12ryt-ipv6.service"

S12RYT_INSTALLER_SOURCE_ONLY=1 . "$ROOT/install.sh"

offline_installer_locked() {
    work_dir=$(mktemp -d) || return 1
    stage="$work_dir/stage"
    if ! mkdir -p "$stage/deploy/systemd" ||
        ! cp "$BINARY" "$stage/s12ryt-ipv6" ||
        ! cp "$UNIT_SOURCE" "$stage/deploy/systemd/s12ryt-ipv6.service"; then
        rm -rf "$work_dir"
        return 1
    fi
    result=0
    install_staged_release "$stage" || result=$?
    rm -rf "$work_dir"
    return "$result"
}

offline_installer_main() {
    require_root || return 1
    [ -f "$BINARY" ] || {
        echo "binary not found: $BINARY" >&2
        return 1
    }
    [ -f "$UNIT_SOURCE" ] || {
        echo "systemd unit not found: $UNIT_SOURCE" >&2
        return 1
    }
    os_release=${OS_RELEASE_FILE:-/etc/os-release}
    if ! validate_os_release "$os_release"; then
        echo "supported systems: Debian 12/13 or Ubuntu 24.04" >&2
        return 1
    fi
    if ! validate_data_directory "$DATA_DIR"; then
        echo "DATA_DIR must be a safe absolute path" >&2
        return 1
    fi
    if [ -n "$MANAGEMENT_PORT" ] && ! validate_management_port "$MANAGEMENT_PORT"; then
        echo "MANAGEMENT_PORT must be between 1 and 65535" >&2
        return 1
    fi
    detect_system_arch >/dev/null || {
        echo "supported architectures: amd64 or arm64" >&2
        return 1
    }
    ensure_dependencies || return 1
    if ! command -v systemctl >/dev/null 2>&1 || ! command -v journalctl >/dev/null 2>&1; then
        echo "systemd and journalctl are required" >&2
        return 1
    fi
    acquire_installer_lock || return 1
    result=0
    offline_installer_locked || result=$?
    release_installer_lock || result=1
    return "$result"
}

offline_installer_main
