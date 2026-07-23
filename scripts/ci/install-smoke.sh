#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
DIST_DIR=${1:-"$ROOT_DIR/dist"}
INSTALLER="$ROOT_DIR/install.sh"

case "$(uname -m)" in
	x86_64) ARCH=amd64 ;;
	aarch64|arm64) ARCH=arm64 ;;
	*)
		echo "unsupported smoke-test architecture: $(uname -m)" >&2
		exit 1
		;;
esac

ASSET="muxlm-linux-$ARCH"
LEGACY_ASSET="providerdeck-linux-$ARCH"
SOURCE_ASSET="$DIST_DIR/$ASSET"
SOURCE_LEGACY_ASSET="$DIST_DIR/$LEGACY_ASSET"
SOURCE_SUMS="$DIST_DIR/SHA256SUMS"

for path in "$INSTALLER" "$SOURCE_ASSET" "$SOURCE_LEGACY_ASSET" "$SOURCE_SUMS"; do
	if [ ! -f "$path" ]; then
		echo "required smoke-test input is missing: $path" >&2
		exit 1
	fi
done

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

assert_same_file() {
	[ "$(sha256_file "$1")" = "$(sha256_file "$2")" ] || {
		echo "files differ: $1 and $2" >&2
		exit 1
	}
}

assert_release_checksum() {
	local asset=$1
	local expected
	local actual
	expected=$(awk -v asset="$asset" '$2 == asset { print $1; exit }' "$SOURCE_SUMS")
	[ -n "$expected" ] || {
		echo "release checksum is missing $asset" >&2
		exit 1
	}
	actual=$(sha256_file "$DIST_DIR/$asset")
	[ "$actual" = "$expected" ] || {
		echo "release checksum does not match $asset" >&2
		exit 1
	}
}

assert_release_checksum "$ASSET"
assert_release_checksum "$LEGACY_ASSET"

TEST_ROOT=$(mktemp -d)
cleanup() {
	rm -rf "$TEST_ROOT"
}
trap cleanup EXIT

# Dependency failures must be reported before the installer attempts network or
# filesystem work. --help must remain usable even with an otherwise empty PATH.
EMPTY_PATH="$TEST_ROOT/empty-path"
mkdir -p "$EMPTY_PATH"
PATH="$EMPTY_PATH" /bin/bash "$INSTALLER" --help >"$EMPTY_PATH/help.log"
grep -q -- '--install-deps' "$EMPTY_PATH/help.log"
if env PATH="$EMPTY_PATH" HOME="$TEST_ROOT/empty-home" /bin/bash "$INSTALLER" >"$EMPTY_PATH/missing.log" 2>&1; then
	echo "installer ignored missing dependencies" >&2
	exit 1
fi
grep -q '还缺这些工具' "$EMPTY_PATH/missing.log"
grep -q -- '--install-deps' "$EMPTY_PATH/missing.log"
if env PATH="$EMPTY_PATH" HOME="$TEST_ROOT/empty-home" /bin/bash "$INSTALLER" --install-deps >"$EMPTY_PATH/noninteractive.log" 2>&1; then
	echo "installer changed dependencies without interactive confirmation" >&2
	exit 1
fi
grep -q '这里无法确认，已停止；系统没有改动' "$EMPTY_PATH/noninteractive.log"

REPO=ci/MuxLM
TAG=v0.0.0-smoke
API_ROOT="$TEST_ROOT/api"
GITHUB_ROOT="$TEST_ROOT/github"
RELEASE_DIR="$GITHUB_ROOT/$REPO/releases/download/$TAG"
mkdir -p "$API_ROOT/repos/$REPO/releases" "$RELEASE_DIR"
printf '{"tag_name":"%s"}\n' "$TAG" > "$API_ROOT/repos/$REPO/releases/latest"
cp "$SOURCE_ASSET" "$SOURCE_LEGACY_ASSET" "$SOURCE_SUMS" "$RELEASE_DIR/"

run_installer() {
	local bindir=$1
	local output=$2
	shift 2
	env \
		HOME="$TEST_ROOT/home" \
		BINDIR="$bindir" \
		GITHUB="file://$GITHUB_ROOT" \
		GITHUB_API="file://$API_ROOT" \
		REPO="$REPO" \
		"$@" \
		bash "$INSTALLER" >"$output" 2>&1
}

assert_links() {
	local bindir=$1
	local entry
	for entry in cdx cld opc providerdeck ez-switch; do
		[ -L "$bindir/$entry" ] || {
			echo "installer did not create $entry as a symlink" >&2
			exit 1
		}
		[ "$(readlink "$bindir/$entry")" = muxlm ] || {
			echo "$entry points to an unexpected target" >&2
			exit 1
		}
	done
}

assert_no_installer_temps() {
	local bindir=$1
	local leftovers=()
	shopt -s nullglob
	leftovers=(
		"$bindir"/.muxlm.??????
		"$bindir"/.muxlm-sums.??????
		"$bindir"/.muxlm-marker.??????
	)
	shopt -u nullglob
	if [ "${#leftovers[@]}" -ne 0 ]; then
		echo "installer left temporary files behind: ${leftovers[*]}" >&2
		exit 1
	fi
}

INSTALL_DIR="$TEST_ROOT/bin"
INSTALL_LOG="$TEST_ROOT/install.log"
run_installer "$INSTALL_DIR" "$INSTALL_LOG"
assert_same_file "$SOURCE_ASSET" "$INSTALL_DIR/muxlm"
assert_links "$INSTALL_DIR"
[ "$(cat "$INSTALL_DIR/.muxlm-install.sha256")" = "$(sha256_file "$SOURCE_ASSET")" ]
[ "$(stat -c '%a' "$INSTALL_DIR/muxlm")" = 755 ]
[ "$(stat -c '%a' "$INSTALL_DIR/.muxlm-install.sha256")" = 600 ]
HOME="$TEST_ROOT/runtime-home" "$INSTALL_DIR/muxlm" doctor >/dev/null

# A second run must recognize its own marker and safely replace the managed copy.
run_installer "$INSTALL_DIR" "$TEST_ROOT/reinstall.log"
assert_same_file "$SOURCE_ASSET" "$INSTALL_DIR/muxlm"
assert_links "$INSTALL_DIR"

# A bad checksum must fail without replacing the already installed binary.
printf 'tampered release asset\n' > "$RELEASE_DIR/$ASSET"
if run_installer "$INSTALL_DIR" "$TEST_ROOT/bad-checksum.log"; then
	echo "installer accepted an asset with a bad checksum" >&2
	exit 1
fi
grep -q '下载文件检查失败' "$TEST_ROOT/bad-checksum.log"
assert_same_file "$SOURCE_ASSET" "$INSTALL_DIR/muxlm"
assert_no_installer_temps "$INSTALL_DIR"
cp "$SOURCE_ASSET" "$RELEASE_DIR/$ASSET"

# If the canonical asset is unavailable, the published compatibility asset works.
rm "$RELEASE_DIR/$ASSET"
LEGACY_DIR="$TEST_ROOT/legacy-bin"
run_installer "$LEGACY_DIR" "$TEST_ROOT/legacy.log"
assert_same_file "$SOURCE_LEGACY_ASSET" "$LEGACY_DIR/muxlm"
grep -q '主下载文件暂不可用，改用' "$TEST_ROOT/legacy.log"
assert_links "$LEGACY_DIR"
cp "$SOURCE_ASSET" "$RELEASE_DIR/$ASSET"

# Existing unmanaged files and unrelated command links are never overwritten.
UNMANAGED_DIR="$TEST_ROOT/unmanaged-bin"
mkdir -p "$UNMANAGED_DIR"
printf 'keep me\n' > "$UNMANAGED_DIR/muxlm"
if run_installer "$UNMANAGED_DIR" "$TEST_ROOT/unmanaged.log"; then
	echo "installer overwrote an unmanaged binary" >&2
	exit 1
fi
[ "$(cat "$UNMANAGED_DIR/muxlm")" = 'keep me' ]

CONFLICT_DIR="$TEST_ROOT/conflict-bin"
mkdir -p "$CONFLICT_DIR"
ln -s /tmp/not-muxlm "$CONFLICT_DIR/cld"
if run_installer "$CONFLICT_DIR" "$TEST_ROOT/conflict.log"; then
	echo "installer overwrote an unrelated command link" >&2
	exit 1
fi
[ "$(readlink "$CONFLICT_DIR/cld")" = /tmp/not-muxlm ]

# A marker symlink could redirect a checksum write, so it must be rejected.
MARKER_DIR="$TEST_ROOT/marker-bin"
VICTIM="$TEST_ROOT/victim"
mkdir -p "$MARKER_DIR"
printf 'do not change\n' > "$VICTIM"
ln -s "$VICTIM" "$MARKER_DIR/.muxlm-install.sha256"
if run_installer "$MARKER_DIR" "$TEST_ROOT/marker.log"; then
	echo "installer accepted a marker symlink" >&2
	exit 1
fi
[ "$(cat "$VICTIM")" = 'do not change' ]

DISTRO=$(grep '^PRETTY_NAME=' /etc/os-release | head -1)
DISTRO=${DISTRO#*=}
DISTRO=${DISTRO#\"}
DISTRO=${DISTRO%\"}
echo "install smoke test passed on $DISTRO ($(uname -m))"
