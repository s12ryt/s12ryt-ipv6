#!/bin/sh
set -eu

PROJECT_NAME=s12ryt-ipv6
SERVICE_NAME=s12ryt-ipv6.service
BINARY_TARGET=${BINARY_TARGET:-/usr/local/bin/s12ryt-ipv6}
UNIT_TARGET=${UNIT_TARGET:-/etc/systemd/system/s12ryt-ipv6.service}
DATA_DIR=${DATA_DIR:-/etc/s12ryt-ipv6}
MANAGEMENT_PORT=${MANAGEMENT_PORT:-}
HEALTH_TIMEOUT=${HEALTH_TIMEOUT:-120}
INSTALL_LOCK_PATH=/run/lock/s12ryt-ipv6-installer.lock

normalize_arch() {
    case "$1" in
        x86_64 | amd64) printf '%s\n' amd64 ;;
        aarch64 | arm64) printf '%s\n' arm64 ;;
        *) return 1 ;;
    esac
}

validate_version() {
    printf '%s\n' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'
}

validate_management_port() {
    case "$1" in
        '' | *[!0-9]*) return 1 ;;
    esac
    [ "$1" -ge 1 ] 2>/dev/null && [ "$1" -le 65535 ] 2>/dev/null
}

validate_data_directory() {
    case "$1" in
        / | */../* | */.. | */./* | */.) return 1 ;;
        /*)
            case "$1" in
                *[!A-Za-z0-9._/-]* | *'//'*) return 1 ;;
            esac
            return 0
            ;;
        *) return 1 ;;
    esac
}

validate_os_release() {
    file=$1
    [ -f "$file" ] || return 1
    os_id=$(sed -n 's/^ID=//p' "$file" | head -n 1 | tr -d '"')
    version_id=$(sed -n 's/^VERSION_ID=//p' "$file" | head -n 1 | tr -d '"')
    case "$os_id:$version_id" in
        debian:12 | debian:13 | ubuntu:24.04) return 0 ;;
        *) return 1 ;;
    esac
}

release_arch_name() {
    case "$1" in
        amd64) printf '%s\n' x86_64 ;;
        arm64) printf '%s\n' arm64 ;;
        *) return 1 ;;
    esac
}

release_base_name() {
    version=${1#v}
    arch=$(release_arch_name "$2") || return 1
    printf '%s_%s_Linux_%s\n' "$PROJECT_NAME" "$version" "$arch"
}

release_archive_name() {
    printf '%s.tar.gz\n' "$(release_base_name "$1" "$2")"
}

release_binary_name() {
    release_base_name "$1" "$2"
}

verify_release_checksum() {
    checksums=$1
    asset_path=$2
    asset_name=$3
    expected=$(awk -v asset="$asset_name" '$2 == asset { print $1 }' "$checksums")
    case "$expected" in
        *[!0-9a-fA-F]* | '') return 1 ;;
    esac
    [ "${#expected}" -eq 64 ] || return 1
    actual=$(sha256sum "$asset_path" | awk '{print $1}')
    [ "$actual" = "$expected" ]
}

health_status() {
    response=$1
    status=$(printf '%s\n' "$response" | sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
    case "$status" in
        healthy | degraded) printf '%s\n' "$status" ;;
        *) return 1 ;;
    esac
}

agent_status_health() {
    response=$1
    case "$response" in
        *'
'*) return 1 ;;
    esac
	case "$response" in
		'{"ok":true,"data":{'*'}}') ;;
		*) return 1 ;;
	esac
	case "$response" in
		*'"error"'*) return 1 ;;
	esac
    status=$(printf '%s\n' "$response" | sed -n 's/.*"health"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
    case "$status" in
        healthy | degraded) printf '%s\n' "$status" ;;
        *) return 1 ;;
    esac
}

render_systemd_unit() {
    source_path=$1
    destination_path=$2
    data_directory=$3
    validate_data_directory "$data_directory" || return 1
    escaped=$(printf '%s\n' "$data_directory" | sed 's/[&|]/\\&/g')
    sed "s|/etc/s12ryt-ipv6|$escaped|g" "$source_path" >"$destination_path"
}

download_file() {
    curl --proto '=https' --tlsv1.2 -fL --retry 3 --retry-delay 2 "$1" -o "$2"
}

curl_effective_url() {
    curl --proto '=https' --tlsv1.2 -fsSL -o /dev/null -w '%{url_effective}\n' "$1"
}

resolve_release_version() {
    requested=${VERSION:-latest}
    if [ -n "$requested" ] && [ "$requested" != latest ]; then
        validate_version "$requested" || return 1
        printf '%s\n' "$requested"
        return 0
    fi
    effective=$(curl_effective_url "https://github.com/s12ryt/s12ryt-ipv6/releases/latest") || return 1
    latest=${effective##*/}
    validate_version "$latest" || return 1
    printf '%s\n' "$latest"
}

download_release() {
    version=$1
    arch=$2
    work_dir=$3
    asset=$(release_archive_name "$version" "$arch") || return 1
    release_url="https://github.com/s12ryt/s12ryt-ipv6/releases/download/$version"
    download_dir="$work_dir/download"
    stage_dir="$work_dir/stage"
    mkdir -p "$download_dir" "$stage_dir" || return 1
    download_file "$release_url/checksums.txt" "$download_dir/checksums.txt" || return 1
    download_file "$release_url/$asset" "$download_dir/$asset" || return 1
    verify_release_checksum "$download_dir/checksums.txt" "$download_dir/$asset" "$asset" || return 1
    tar -xzf "$download_dir/$asset" -C "$stage_dir" --no-same-owner --no-same-permissions || return 1
    [ -f "$stage_dir/s12ryt-ipv6" ] || return 1
    [ -f "$stage_dir/deploy/systemd/s12ryt-ipv6.service" ] || return 1
    printf '%s\n' "$stage_dir"
}

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        echo "install.sh must run as root" >&2
        return 1
    fi
}

detect_system_arch() {
    normalize_arch "$(uname -m)"
}

ensure_dependencies() {
    missing=0
    for command_name in curl sha256sum tar install nft flock; do
        command -v "$command_name" >/dev/null 2>&1 || missing=1
    done
    if [ "$missing" -eq 0 ]; then
        return 0
    fi
    if ! command -v apt-get >/dev/null 2>&1; then
        echo "apt-get is required to install dependencies" >&2
        return 1
    fi
    apt-get update || return 1
    DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl nftables coreutils tar util-linux || return 1
}

service_stop() {
    had_unit=${1:-0}
    if [ "$had_unit" -eq 1 ]; then
        systemctl stop "$SERVICE_NAME"
        return
    fi
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
}

acquire_installer_lock() {
    lock_directory=$(dirname "$INSTALL_LOCK_PATH")
    install -d -m 0755 "$lock_directory" || return 1
    eval "exec 9>\"\$INSTALL_LOCK_PATH\"" || return 1
    if ! flock -n 9; then
        eval 'exec 9>&-'
        echo "another s12ryt-ipv6 installation is already running" >&2
        return 1
    fi
}

release_installer_lock() {
    flock -u 9 2>/dev/null || true
    eval 'exec 9>&-'
}

service_disable_stop() {
    systemctl disable --now "$SERVICE_NAME" 2>/dev/null || true
}

service_reload() {
    systemctl daemon-reload
}

service_enable_start() {
    systemctl enable --now "$SERVICE_NAME"
}

configure_management_port() {
    "$BINARY_TARGET" config set-management-port --data-dir "$DATA_DIR" --port "$1" >/dev/null
}

read_management_port() {
    "$BINARY_TARGET" config get-management-port --data-dir "$DATA_DIR"
}

wait_for_health() {
    port=$1
    attempt=0
    while [ "$attempt" -lt "$HEALTH_TIMEOUT" ]; do
        response=$(curl -fsS --max-time 2 "http://127.0.0.1:$port/healthz" 2>/dev/null || true)
        if [ -n "$response" ] && health_status "$response" >/dev/null 2>&1; then
            return 0
        fi
        attempt=$((attempt + 1))
        [ "$attempt" -lt "$HEALTH_TIMEOUT" ] && sleep 1
    done
    return 1
}

wait_for_agent() {
    attempt=0
    while [ "$attempt" -lt "$HEALTH_TIMEOUT" ]; do
        response=$("$BINARY_TARGET" agent status --data-dir "$DATA_DIR" --timeout 2s 2>/dev/null || true)
        if [ -n "$response" ] && agent_status_health "$response" >/dev/null 2>&1; then
            return 0
        fi
        attempt=$((attempt + 1))
        [ "$attempt" -lt "$HEALTH_TIMEOUT" ] && sleep 1
    done
    return 1
}

show_agent_quickstart() {
    printf '%s\n' 'agent CLI quickstart:'
    printf '  sudo s12ryt-ipv6 agent status --data-dir %s\n' "$DATA_DIR"
    printf '  sudo s12ryt-ipv6 agent schema --data-dir %s\n' "$DATA_DIR"
    printf '  sudo s12ryt-ipv6 agent export --format json --data-dir %s\n' "$DATA_DIR"
    printf '  sudo s12ryt-ipv6 agent export --format yaml --data-dir %s\n' "$DATA_DIR"
    printf '  sudo s12ryt-ipv6 agent export --format yaml --data-dir %s | sudo s12ryt-ipv6 agent apply --format yaml --dry-run --data-dir %s\n' "$DATA_DIR" "$DATA_DIR"
    printf '  sudo s12ryt-ipv6 agent apply --format yaml --file ./agent-config.yaml --dry-run --data-dir %s\n' "$DATA_DIR"
}

open_management_firewall() {
    port=$1
    previous_port=${2:-}
    if ! command -v ufw >/dev/null 2>&1; then
        echo "UFW is not installed; management port $port was not opened automatically" >&2
        return 0
    fi
    if ! ufw status 2>/dev/null | grep -q '^Status: active'; then
        echo "UFW is not active; management port $port was not opened automatically" >&2
        return 0
    fi
    if ! ufw allow "$port/tcp" comment 's12ryt-ipv6 management' >/dev/null; then
        echo "warning: failed to add UFW rule for TCP port $port" >&2
        return 0
    fi
    if [ -n "$previous_port" ] && [ "$previous_port" != "$port" ]; then
        echo "warning: the previous UFW rule for TCP port $previous_port was preserved" >&2
    fi
}

show_initial_password() {
    started_at=$1
    attempt=0
    while [ "$attempt" -lt "$HEALTH_TIMEOUT" ]; do
        password=$(journalctl -u "$SERVICE_NAME" --since "$started_at" --no-pager -o cat 2>/dev/null |
            sed -n 's/^initial admin password: //p' | tail -n 1)
        if [ -n "$password" ]; then
            printf 'initial admin password: %s\n' "$password"
            return 0
        fi
        attempt=$((attempt + 1))
        [ "$attempt" -lt "$HEALTH_TIMEOUT" ] && sleep 1
    done
    echo "initial password was not found; run: sudo s12ryt-ipv6 admin reset-password --data-dir $DATA_DIR" >&2
    return 0
}

restore_file() {
    existed=$1
    backup=$2
    target=$3
    if [ "$existed" -eq 1 ]; then
        install -D -m "$4" "$backup" "$target"
    else
        rm -f "$target"
    fi
}

rollback_staged_release() {
    backup_dir=$1
    had_binary=$2
    had_unit=$3
    had_config=$4
    old_port=$5

    service_disable_stop
    restore_file "$had_binary" "$backup_dir/binary" "$BINARY_TARGET" 0755 || true
    restore_file "$had_unit" "$backup_dir/unit" "$UNIT_TARGET" 0644 || true
    restore_file "$had_config" "$backup_dir/config" "$DATA_DIR/config.yaml" 0600 || true
    service_reload || true
    if [ "$had_binary" -eq 1 ] && [ "$had_unit" -eq 1 ]; then
        if service_enable_start; then
            if ! wait_for_health "$old_port"; then
                echo "warning: the previous version was restored but did not become healthy" >&2
            fi
        else
            echo "warning: the previous version was restored but could not be started" >&2
        fi
    fi
}

install_staged_release() {
    stage=$1
    staged_binary="$stage/s12ryt-ipv6"
    staged_unit="$stage/deploy/systemd/s12ryt-ipv6.service"
    [ -f "$staged_binary" ] || return 1
    [ -f "$staged_unit" ] || return 1
    validate_data_directory "$DATA_DIR" || return 1
    if [ -n "$MANAGEMENT_PORT" ]; then
        validate_management_port "$MANAGEMENT_PORT" || return 1
    fi

    backup_dir=$(mktemp -d) || return 1
    had_binary=0
    had_unit=0
    had_config=0
    had_password=0
    if [ -f "$BINARY_TARGET" ]; then
        had_binary=1
        cp "$BINARY_TARGET" "$backup_dir/binary" || {
            rm -rf "$backup_dir"
            return 1
        }
    fi
    if [ -f "$UNIT_TARGET" ]; then
        had_unit=1
        cp "$UNIT_TARGET" "$backup_dir/unit" || {
            rm -rf "$backup_dir"
            return 1
        }
    fi
    if [ -f "$DATA_DIR/config.yaml" ]; then
        had_config=1
        cp "$DATA_DIR/config.yaml" "$backup_dir/config" || {
            rm -rf "$backup_dir"
            return 1
        }
    fi
    [ -f "$DATA_DIR/admin-password.yaml" ] && had_password=1

    if ! service_stop "$had_unit"; then
        rm -rf "$backup_dir"
        return 1
    fi
    install -d -m 0700 "$DATA_DIR" "$(dirname "$BINARY_TARGET")" "$(dirname "$UNIT_TARGET")" || {
        rollback_staged_release "$backup_dir" "$had_binary" "$had_unit" "$had_config" 34466
        rm -rf "$backup_dir"
        return 1
    }
    install -m 0755 "$staged_binary" "$BINARY_TARGET" || {
        rollback_staged_release "$backup_dir" "$had_binary" "$had_unit" "$had_config" 34466
        rm -rf "$backup_dir"
        return 1
    }
    rendered_unit="$backup_dir/rendered.service"
    if ! render_systemd_unit "$staged_unit" "$rendered_unit" "$DATA_DIR" || ! install -m 0644 "$rendered_unit" "$UNIT_TARGET"; then
        rollback_staged_release "$backup_dir" "$had_binary" "$had_unit" "$had_config" 34466
        rm -rf "$backup_dir"
        return 1
    fi

    old_port=34466
    if [ "$had_config" -eq 1 ]; then
        old_port=$(read_management_port) || {
            rollback_staged_release "$backup_dir" "$had_binary" "$had_unit" "$had_config" 34466
            rm -rf "$backup_dir"
            return 1
        }
    fi
    if [ -n "$MANAGEMENT_PORT" ] && ! configure_management_port "$MANAGEMENT_PORT"; then
        rollback_staged_release "$backup_dir" "$had_binary" "$had_unit" "$had_config" "$old_port"
        rm -rf "$backup_dir"
        return 1
    fi
    new_port=$(read_management_port) || {
        rollback_staged_release "$backup_dir" "$had_binary" "$had_unit" "$had_config" "$old_port"
        rm -rf "$backup_dir"
        return 1
    }
    started_at=$(date --iso-8601=seconds)
    if ! service_reload || ! service_enable_start || ! wait_for_health "$new_port" || ! wait_for_agent; then
        rollback_staged_release "$backup_dir" "$had_binary" "$had_unit" "$had_config" "$old_port"
        rm -rf "$backup_dir"
        return 1
    fi

    open_management_firewall "$new_port" "$old_port"
    if [ "$had_password" -eq 0 ]; then
        show_initial_password "$started_at"
    fi
    show_agent_quickstart
    rm -rf "$backup_dir"
    return 0
}

installer_main_locked() {
    arch=$1
    version=$(resolve_release_version) || {
        echo "failed to resolve a valid GitHub Release version" >&2
        return 1
    }
    work_dir=$(mktemp -d) || return 1
    stage=$(download_release "$version" "$arch" "$work_dir") || {
        rm -rf "$work_dir"
        echo "release download or checksum verification failed" >&2
        return 1
    }
    if ! install_staged_release "$stage"; then
        rm -rf "$work_dir"
        echo "installation failed; previous installation was restored when available" >&2
        return 1
    fi
    rm -rf "$work_dir"
    echo "$PROJECT_NAME $version installed successfully"
}

installer_main() {
    require_root || return 1
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
    arch=$(detect_system_arch) || {
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
    installer_main_locked "$arch" || result=$?
    release_installer_lock || result=1
    return "$result"
}

if [ "${S12RYT_INSTALLER_SOURCE_ONLY:-0}" != 1 ]; then
    installer_main
fi
