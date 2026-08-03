#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
CONFIG="$ROOT/.goreleaser.yaml"
WORKFLOW="$ROOT/.github/workflows/release.yml"
README="$ROOT/README.md"

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

[ -f "$CONFIG" ] || fail ".goreleaser.yaml is missing"
[ -f "$WORKFLOW" ] || fail "release workflow is missing"
[ -f "$README" ] || fail "README.md is missing"

grep -Eq '^version:[[:space:]]*2$' "$CONFIG" || fail "GoReleaser v2 config is required"
grep -Fq 'main: ./cmd/s12ryt-ipv6' "$CONFIG" || fail "release build does not target the service CLI"
grep -Fq -- '- linux' "$CONFIG" || fail "Linux build target is missing"
grep -Fq -- '- amd64' "$CONFIG" || fail "amd64 build target is missing"
grep -Fq -- '- arm64' "$CONFIG" || fail "arm64 build target is missing"
grep -Fq -- '- tar.gz' "$CONFIG" || fail "tar.gz release format is missing"
grep -Fq -- '- binary' "$CONFIG" || fail "raw binary release format is missing"
grep -Fq 'name_template: checksums.txt' "$CONFIG" || fail "checksums.txt is missing"
grep -Fq 'install.sh' "$CONFIG" || fail "archive does not contain the one-line installer"
grep -Fq 'deploy/systemd/s12ryt-ipv6.service' "$CONFIG" || fail "archive does not contain the systemd unit"
grep -Fq '.Version' "$CONFIG" || fail "release asset names are not versioned"
grep -Fq 'x86_64' "$CONFIG" || fail "amd64 release name does not follow GoReleaser convention"

grep -Fq 'workflow_dispatch:' "$WORKFLOW" || fail "manual release trigger is missing"
grep -Fq "tags:" "$WORKFLOW" || fail "tag release trigger is missing"
grep -Fq -- "- 'v*'" "$WORKFLOW" || fail "v* tag filter is missing"
grep -Fq 'contents: write' "$WORKFLOW" || fail "release workflow lacks contents write permission"
grep -Fq 'fetch-depth: 0' "$WORKFLOW" || fail "release workflow does not fetch tag history"
grep -Fq 'npm ci' "$WORKFLOW" || fail "release workflow does not install frontend dependencies"
grep -Fq 'npm test -- --run' "$WORKFLOW" || fail "release workflow does not run frontend tests"
grep -Fq 'npm run lint' "$WORKFLOW" || fail "release workflow does not lint frontend"
grep -Fq 'npm run build' "$WORKFLOW" || fail "release workflow does not build embedded frontend"
grep -Fq 'go test ./...' "$WORKFLOW" || fail "release workflow does not run Go tests"
grep -Fq 'run: go test .' "$WORKFLOW" || fail "release workflow does not test the web embed module"
grep -Fq 'bash deploy/install_test.sh' "$WORKFLOW" || fail "release workflow does not test the installer"
grep -Fq 'bash deploy/release_test.sh' "$WORKFLOW" || fail "release workflow does not test release contracts"
grep -Fq 'args: check' "$WORKFLOW" || fail "release workflow does not validate GoReleaser before tagging"
grep -Fq 'goreleaser/goreleaser-action@v6' "$WORKFLOW" || fail "GoReleaser action is missing"
grep -Fq 'release --clean' "$WORKFLOW" || fail "GoReleaser release command is missing"
grep -Fq 'git tag -a' "$WORKFLOW" || fail "manual workflow does not create the requested tag"
grep -Fq 'CREATE_RELEASE_TAG=' "$WORKFLOW" || fail "manual workflow does not track whether a tag must be created"
grep -Fq 'git rev-list -n 1 "$tag"' "$WORKFLOW" || fail "existing tag recovery does not verify the target commit"
grep -Fq 'gh release view "$tag"' "$WORKFLOW" || fail "existing tag recovery does not reject an existing Release"
grep -Fq "env.CREATE_RELEASE_TAG == 'true'" "$WORKFLOW" || fail "manual tag creation is not guarded by the validation result"
grep -Fq 'GORELEASER_CURRENT_TAG: ${{ env.RELEASE_TAG }}' "$WORKFLOW" || fail "GoReleaser is not pinned to the requested tag"
if grep -Fq "if: github.event_name == 'push'" "$WORKFLOW"; then
    fail "manual workflow still skips GitHub Release publishing"
fi
GO_TEST_LINE=$(grep -nF 'go test ./...' "$WORKFLOW" | head -n 1 | cut -d: -f1)
INSTALL_TEST_LINE=$(grep -nF 'bash deploy/install_test.sh' "$WORKFLOW" | head -n 1 | cut -d: -f1)
RELEASE_CHECK_LINE=$(grep -nF 'args: check' "$WORKFLOW" | head -n 1 | cut -d: -f1)
TAG_LINE=$(grep -nF 'git tag -a' "$WORKFLOW" | head -n 1 | cut -d: -f1)
PUBLISH_LINE=$(grep -nF 'release --clean' "$WORKFLOW" | head -n 1 | cut -d: -f1)
if [ -z "$GO_TEST_LINE" ] || [ -z "$INSTALL_TEST_LINE" ] || [ -z "$RELEASE_CHECK_LINE" ] || [ -z "$TAG_LINE" ] ||
    [ "$TAG_LINE" -le "$GO_TEST_LINE" ] || [ "$TAG_LINE" -le "$INSTALL_TEST_LINE" ] || [ "$TAG_LINE" -le "$RELEASE_CHECK_LINE" ]; then
    fail "manual tag must be created only after all tests pass"
fi
if [ -z "$PUBLISH_LINE" ] || [ "$PUBLISH_LINE" -le "$TAG_LINE" ]; then
    fail "GitHub Release must be published after manual tag creation"
fi

grep -Fq 'https://raw.githubusercontent.com/s12ryt/s12ryt-ipv6/main/install.sh' "$README" || fail "README lacks the one-line installer"
grep -Fq 'VERSION=v1.2.3' "$README" || fail "README lacks a pinned-version installer example"
grep -Fq 'MANAGEMENT_PORT=45555' "$README" || fail "README lacks a management-port installer example"

echo "release configuration tests passed"
