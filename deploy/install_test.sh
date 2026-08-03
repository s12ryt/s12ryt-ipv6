#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
INSTALLER="$ROOT/install.sh"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT HUP INT TERM

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

assert_equal() {
    if [ "$1" != "$2" ]; then
        fail "got '$1', want '$2'"
    fi
}

if [ ! -f "$INSTALLER" ]; then
    fail "root install.sh is missing"
fi

if ! grep -Fxq 'INSTALL_LOCK_PATH=/run/lock/s12ryt-ipv6-installer.lock' "$INSTALLER"; then
    fail "installer lock path must be global and must not be environment-overridable"
fi

S12RYT_INSTALLER_SOURCE_ONLY=1 . "$INSTALLER"

assert_equal "$(normalize_arch x86_64)" "amd64"
assert_equal "$(normalize_arch amd64)" "amd64"
assert_equal "$(normalize_arch aarch64)" "arm64"
assert_equal "$(normalize_arch arm64)" "arm64"
if normalize_arch i686 >/dev/null 2>&1; then
    fail "unsupported architecture was accepted"
fi

assert_equal "$(release_archive_name v1.2.3 amd64)" "s12ryt-ipv6_1.2.3_Linux_x86_64.tar.gz"
assert_equal "$(release_archive_name v1.2.3 arm64)" "s12ryt-ipv6_1.2.3_Linux_arm64.tar.gz"
assert_equal "$(release_binary_name v1.2.3 amd64)" "s12ryt-ipv6_1.2.3_Linux_x86_64"

validate_version v1.2.3
if validate_version 1.2.3 >/dev/null 2>&1 || validate_version v1.2 >/dev/null 2>&1 || validate_version 'v1.2.3;rm' >/dev/null 2>&1; then
    fail "invalid release version was accepted"
fi
validate_management_port 1
validate_management_port 65535
if validate_management_port 0 >/dev/null 2>&1 || validate_management_port 65536 >/dev/null 2>&1 || validate_management_port abc >/dev/null 2>&1; then
    fail "invalid management port was accepted"
fi
validate_data_directory /etc/s12ryt-ipv6
if validate_data_directory / >/dev/null 2>&1 ||
    validate_data_directory /etc/../tmp >/dev/null 2>&1 ||
    validate_data_directory /etc/./s12ryt-ipv6 >/dev/null 2>&1 ||
    validate_data_directory relative/path >/dev/null 2>&1 ||
    validate_data_directory '/path with spaces' >/dev/null 2>&1; then
    fail "unsafe data directory was accepted"
fi

cat >"$TEMP_DIR/debian" <<'EOF'
ID=debian
VERSION_ID="12"
EOF
validate_os_release "$TEMP_DIR/debian"
cat >"$TEMP_DIR/ubuntu" <<'EOF'
ID=ubuntu
VERSION_ID="24.04"
EOF
validate_os_release "$TEMP_DIR/ubuntu"
cat >"$TEMP_DIR/unsupported" <<'EOF'
ID=debian
VERSION_ID="11"
EOF
if validate_os_release "$TEMP_DIR/unsupported" >/dev/null 2>&1; then
    fail "unsupported operating system was accepted"
fi

printf 'release payload\n' >"$TEMP_DIR/archive.tar.gz"
HASH=$(sha256sum "$TEMP_DIR/archive.tar.gz" | awk '{print $1}')
printf '%s  archive.tar.gz\n' "$HASH" >"$TEMP_DIR/checksums.txt"
verify_release_checksum "$TEMP_DIR/checksums.txt" "$TEMP_DIR/archive.tar.gz" "archive.tar.gz"
if verify_release_checksum "$TEMP_DIR/checksums.txt" "$TEMP_DIR/archive.tar.gz" "missing.tar.gz" >/dev/null 2>&1; then
    fail "checksum verification accepted a missing asset entry"
fi
printf '%s  archive.tar.gz\n%s  archive.tar.gz\n' "$HASH" "$HASH" >"$TEMP_DIR/duplicate-checksums.txt"
if verify_release_checksum "$TEMP_DIR/duplicate-checksums.txt" "$TEMP_DIR/archive.tar.gz" "archive.tar.gz" >/dev/null 2>&1; then
    fail "checksum verification accepted duplicate asset entries"
fi
printf 'tampered\n' >>"$TEMP_DIR/archive.tar.gz"
if verify_release_checksum "$TEMP_DIR/checksums.txt" "$TEMP_DIR/archive.tar.gz" "archive.tar.gz" >/dev/null 2>&1; then
    fail "tampered release asset passed checksum verification"
fi

assert_equal "$(health_status '{"status":"healthy"}')" "healthy"
assert_equal "$(health_status '{ "status": "degraded" }')" "degraded"
if health_status '{"status":"unhealthy"}' >/dev/null 2>&1 || health_status 'not-json' >/dev/null 2>&1; then
    fail "unacceptable health response was accepted"
fi

UFW_EVENTS="$TEMP_DIR/ufw-events"
UFW_ACTIVE=0
ufw() {
    if [ "$1" = status ]; then
        if [ "$UFW_ACTIVE" -eq 1 ]; then
            printf '%s\n' 'Status: active'
        else
            printf '%s\n' 'Status: inactive'
        fi
        return 0
    fi
    printf '%s\n' "$*" >>"$UFW_EVENTS"
}
open_management_firewall 34466 '' 2>/dev/null
[ ! -e "$UFW_EVENTS" ] || fail "inactive UFW was modified"
UFW_ACTIVE=1
open_management_firewall 45555 34466 2>/dev/null
grep -Fq 'allow 45555/tcp comment s12ryt-ipv6 management' "$UFW_EVENTS" || fail "active UFW was not opened"
if grep -Eq '(^|[[:space:]])enable($|[[:space:]])' "$UFW_EVENTS"; then
    fail "installer enabled UFW"
fi

JOURNAL_EVENTS="$TEMP_DIR/journal-events"
journalctl() {
    printf '%s\n' "$*" >>"$JOURNAL_EVENTS"
    printf '%s\n' 'initial admin password: generated-secret'
}
HEALTH_TIMEOUT=1
assert_equal "$(show_initial_password '2026-08-03T12:34:56+00:00')" "initial admin password: generated-secret"
grep -Fq -- '--since 2026-08-03T12:34:56+00:00' "$JOURNAL_EVENTS" || fail "journal query was not limited to this service start"

render_systemd_unit "$ROOT/deploy/systemd/s12ryt-ipv6.service" "$TEMP_DIR/service" "/srv/s12ryt"
grep -Fq 'ExecStart=/usr/local/bin/s12ryt-ipv6 serve --data-dir /srv/s12ryt' "$TEMP_DIR/service" || fail "rendered ExecStart does not use DATA_DIR"
grep -Fq 'ReadWritePaths=/srv/s12ryt' "$TEMP_DIR/service" || fail "rendered ReadWritePaths does not use DATA_DIR"

TRANSACTION_ROOT="$TEMP_DIR/transaction"
mkdir -p "$TRANSACTION_ROOT/stage/deploy/systemd" "$TRANSACTION_ROOT/data"
printf 'new-binary\n' >"$TRANSACTION_ROOT/stage/s12ryt-ipv6"
cat >"$TRANSACTION_ROOT/stage/deploy/systemd/s12ryt-ipv6.service" <<'EOF'
ExecStart=/usr/local/bin/s12ryt-ipv6 serve --data-dir /etc/s12ryt-ipv6
ReadWritePaths=/etc/s12ryt-ipv6
EOF
BINARY_TARGET="$TRANSACTION_ROOT/bin/s12ryt-ipv6"
UNIT_TARGET="$TRANSACTION_ROOT/systemd/s12ryt-ipv6.service"
DATA_DIR="$TRANSACTION_ROOT/data"
MANAGEMENT_PORT=45555
EVENTS="$TRANSACTION_ROOT/events"

service_stop() { printf '%s\n' stop >>"$EVENTS"; }
service_disable_stop() { printf '%s\n' disable-stop >>"$EVENTS"; }
service_reload() { printf '%s\n' reload >>"$EVENTS"; }
service_enable_start() { printf '%s\n' start >>"$EVENTS"; }
configure_management_port() {
    printf 'new-config:%s\n' "$1" >"$DATA_DIR/config.yaml"
    printf 'configure:%s\n' "$1" >>"$EVENTS"
}
read_management_port() {
    case "$(cat "$DATA_DIR/config.yaml" 2>/dev/null || true)" in
        old-config*) printf '%s\n' 34466 ;;
        new-config:*) sed -n 's/^new-config://p' "$DATA_DIR/config.yaml" ;;
        *) printf '%s\n' 34466 ;;
    esac
}
open_management_firewall() { printf 'ufw:%s\n' "$1" >>"$EVENTS"; }
show_initial_password() { printf 'password:%s\n' "$1" >>"$EVENTS"; }

mkdir -p "$(dirname "$BINARY_TARGET")" "$(dirname "$UNIT_TARGET")"
printf 'old-binary\n' >"$BINARY_TARGET"
printf 'old-unit\n' >"$UNIT_TARGET"
printf 'old-config\n' >"$DATA_DIR/config.yaml"
HEALTH_CALLS="$TRANSACTION_ROOT/health-calls"
: >"$HEALTH_CALLS"
wait_for_health() {
    printf 'health:%s\n' "$1" >>"$EVENTS"
    printf 'x\n' >>"$HEALTH_CALLS"
    [ "$(wc -l <"$HEALTH_CALLS")" -gt 1 ]
}

if install_staged_release "$TRANSACTION_ROOT/stage"; then
    fail "failed upgrade reported success"
fi
assert_equal "$(cat "$BINARY_TARGET")" "old-binary"
assert_equal "$(cat "$UNIT_TARGET")" "old-unit"
assert_equal "$(cat "$DATA_DIR/config.yaml")" "old-config"
grep -Fq 'health:45555' "$EVENTS" || fail "new management port was not health checked"
grep -Fq 'health:34466' "$EVENTS" || fail "rolled back service was not health checked"
if grep -Fq 'ufw:' "$EVENTS" || grep -Fq 'password:' "$EVENTS"; then
    fail "failed upgrade changed firewall or displayed a password"
fi

rm -f "$BINARY_TARGET" "$UNIT_TARGET" "$DATA_DIR/config.yaml" "$EVENTS" "$HEALTH_CALLS"
: >"$HEALTH_CALLS"
wait_for_health() {
    printf 'health:%s\n' "$1" >>"$EVENTS"
    return 1
}
if install_staged_release "$TRANSACTION_ROOT/stage"; then
    fail "failed first installation reported success"
fi
[ ! -e "$BINARY_TARGET" ] || fail "failed first installation left binary installed"
[ ! -e "$UNIT_TARGET" ] || fail "failed first installation left systemd unit installed"
[ ! -e "$DATA_DIR/config.yaml" ] || fail "failed first installation left generated config installed"

rm -f "$EVENTS"
wait_for_health() {
    printf 'health:%s\n' "$1" >>"$EVENTS"
    return 0
}
install_staged_release "$TRANSACTION_ROOT/stage"
assert_equal "$(cat "$BINARY_TARGET")" "new-binary"
grep -Fq 'ufw:45555' "$EVENTS" || fail "successful installation did not open the active UFW port"
grep -Fq 'password:' "$EVENTS" || fail "successful first installation did not inspect the startup journal"

printf 'old-binary\n' >"$BINARY_TARGET"
printf 'old-unit\n' >"$UNIT_TARGET"
printf 'old-config\n' >"$DATA_DIR/config.yaml"
service_stop() { printf '%s\n' stop-failed >>"$EVENTS"; return 1; }
if install_staged_release "$TRANSACTION_ROOT/stage"; then
    fail "installation continued after the existing service failed to stop"
fi
assert_equal "$(cat "$BINARY_TARGET")" "old-binary"
assert_equal "$(cat "$UNIT_TARGET")" "old-unit"
assert_equal "$(cat "$DATA_DIR/config.yaml")" "old-config"

service_stop() { printf '%s\n' stop >>"$EVENTS"; }
cp() {
    if [ "$1" = "$BINARY_TARGET" ]; then
        return 1
    fi
    command cp "$@"
}
if install_staged_release "$TRANSACTION_ROOT/stage"; then
    fail "installation continued after the existing binary backup failed"
fi
unset -f cp
assert_equal "$(cat "$BINARY_TARGET")" "old-binary"
assert_equal "$(cat "$UNIT_TARGET")" "old-unit"
assert_equal "$(cat "$DATA_DIR/config.yaml")" "old-config"

curl_effective_url() { printf '%s\n' 'https://github.com/s12ryt/s12ryt-ipv6/releases/tag/v2.3.4'; }
VERSION=latest
assert_equal "$(resolve_release_version)" "v2.3.4"
VERSION=v1.9.0
assert_equal "$(resolve_release_version)" "v1.9.0"
VERSION=invalid
if resolve_release_version >/dev/null 2>&1; then
    fail "invalid explicit VERSION was accepted"
fi

DOWNLOAD_FIXTURE="$TEMP_DIR/download-fixture"
mkdir -p "$DOWNLOAD_FIXTURE/content/deploy/systemd"
printf 'archive-binary\n' >"$DOWNLOAD_FIXTURE/content/s12ryt-ipv6"
cp "$ROOT/deploy/systemd/s12ryt-ipv6.service" "$DOWNLOAD_FIXTURE/content/deploy/systemd/s12ryt-ipv6.service"
tar -czf "$DOWNLOAD_FIXTURE/release.tar.gz" -C "$DOWNLOAD_FIXTURE/content" .
ARCHIVE_NAME=$(release_archive_name v1.2.3 amd64)
ARCHIVE_HASH=$(sha256sum "$DOWNLOAD_FIXTURE/release.tar.gz" | awk '{print $1}')
printf '%s  %s\n' "$ARCHIVE_HASH" "$ARCHIVE_NAME" >"$DOWNLOAD_FIXTURE/checksums.txt"
DOWNLOAD_LOG="$DOWNLOAD_FIXTURE/urls"
download_file() {
    url=$1
    destination=$2
    printf '%s\n' "$url" >>"$DOWNLOAD_LOG"
    case "$url" in
        */checksums.txt) cp "$DOWNLOAD_FIXTURE/checksums.txt" "$destination" ;;
        */"$ARCHIVE_NAME") cp "$DOWNLOAD_FIXTURE/release.tar.gz" "$destination" ;;
        *) return 1 ;;
    esac
}
DOWNLOADED_STAGE=$(download_release v1.2.3 amd64 "$DOWNLOAD_FIXTURE/work")
[ -f "$DOWNLOADED_STAGE/s12ryt-ipv6" ] || fail "verified release archive was not extracted"
grep -Fq "/releases/download/v1.2.3/$ARCHIVE_NAME" "$DOWNLOAD_LOG" || fail "release asset URL is incorrect"

MAIN_LOG="$TEMP_DIR/main-events"
require_root() { printf '%s\n' root >>"$MAIN_LOG"; }
ensure_dependencies() { printf '%s\n' dependencies >>"$MAIN_LOG"; }
acquire_installer_lock() { printf '%s\n' lock >>"$MAIN_LOG"; }
release_installer_lock() { printf '%s\n' unlock >>"$MAIN_LOG"; }
detect_system_arch() { printf '%s\n' amd64; }
download_release() {
    printf 'download:%s:%s\n' "$1" "$2" >>"$MAIN_LOG"
    printf '%s\n' "$TEMP_DIR/main-stage"
}
install_staged_release() { printf 'install:%s\n' "$1" >>"$MAIN_LOG"; }
OS_RELEASE_FILE="$TEMP_DIR/debian"
VERSION=v1.2.3
DATA_DIR=/etc/s12ryt-ipv6
MANAGEMENT_PORT=34466
installer_main >/dev/null
assert_equal "$(sed -n '1p' "$MAIN_LOG")" "root"
grep -Fq 'dependencies' "$MAIN_LOG" || fail "installer did not prepare dependencies"
grep -Fq 'lock' "$MAIN_LOG" || fail "installer did not acquire the installation lock"
grep -Fq 'download:v1.2.3:amd64' "$MAIN_LOG" || fail "installer did not download selected release"
grep -Fq "install:$TEMP_DIR/main-stage" "$MAIN_LOG" || fail "installer did not install the staged release"
assert_equal "$(tail -n 1 "$MAIN_LOG")" "unlock"

rm -f "$MAIN_LOG"
download_release() {
    printf '%s\n' download >>"$MAIN_LOG"
    return 1
}
if installer_main >/dev/null 2>&1; then
    fail "download failure reported installation success"
fi
if grep -q '^install:' "$MAIN_LOG" 2>/dev/null; then
    fail "download failure reached the installation transaction"
fi
assert_equal "$(tail -n 1 "$MAIN_LOG")" "unlock"

OFFLINE_INSTALLER="$ROOT/deploy/install.sh"
grep -Fq 'S12RYT_INSTALLER_SOURCE_ONLY=1' "$OFFLINE_INSTALLER" || fail "offline installer does not reuse root installer validation"
grep -Fq 'install_staged_release' "$OFFLINE_INSTALLER" || fail "offline installer does not reuse transactional installation"

if grep -Eq 'ufw[[:space:]]+(--force[[:space:]]+)?enable' "$INSTALLER"; then
    fail "installer must never enable UFW"
fi
if grep -Eq 'ip[[:space:]]+-6[[:space:]]+route|ip[[:space:]]+route' "$INSTALLER"; then
    fail "installer must never change routes"
fi

echo "install helper tests passed"
