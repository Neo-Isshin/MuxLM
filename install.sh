#!/usr/bin/env bash
# Usage: curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash
set -euo pipefail

usage() {
  printf '%s\n' \
    'MuxLM Installer' \
    '' \
    'Usage:' \
    '  bash install.sh' \
    '  bash install.sh --install-deps' \
    '  curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash' \
    '  curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash -s -- --install-deps' \
    '' \
    'Options:' \
    '  --install-deps  Install missing dependencies after confirmation' \
    '  -h, --help      Show this help'
}

INSTALL_DEPS=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-deps) INSTALL_DEPS=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "✗ Unknown installer option: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

REQUIRED_COMMANDS=(curl uname sed head awk mkdir mktemp readlink chmod mv ln rm)
MISSING_COMMANDS=()

collect_missing_dependencies() {
  MISSING_COMMANDS=()
  local command_name
  for command_name in "${REQUIRED_COMMANDS[@]}"; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      MISSING_COMMANDS+=("$command_name")
    fi
  done
  if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
    MISSING_COMMANDS+=("sha256sum/shasum")
  fi
}

os_release_id() {
  local key value
  if [ ! -r /etc/os-release ]; then
    return
  fi
  while IFS='=' read -r key value; do
    if [ "$key" = "ID" ]; then
      value=${value#\"}
      value=${value%\"}
      printf '%s\n' "$value"
      return
    fi
  done < /etc/os-release
}

dependency_manager() {
  local manager distro
  for manager in apt-get dnf yum apk pacman zypper brew; do
    if command -v "$manager" >/dev/null 2>&1; then
      printf '%s\n' "$manager"
      return
    fi
  done
  distro=$(os_release_id)
  case "$distro" in
    debian|ubuntu|linuxmint|pop) printf '%s\n' apt-get ;;
    fedora|rhel|centos|rocky|almalinux) printf '%s\n' dnf ;;
    alpine) printf '%s\n' apk ;;
    arch|manjaro|endeavouros) printf '%s\n' pacman ;;
    opensuse*|sles) printf '%s\n' zypper ;;
  esac
}

dependency_install_hint() {
  case "$1" in
    apt-get) printf '%s\n' 'sudo apt-get update && sudo apt-get install -y bash ca-certificates curl coreutils gawk sed' ;;
    dnf) printf '%s\n' 'sudo dnf install -y bash ca-certificates curl coreutils gawk sed' ;;
    yum) printf '%s\n' 'sudo yum install -y bash ca-certificates curl coreutils gawk sed' ;;
    apk) printf '%s\n' 'sudo apk add --no-cache bash ca-certificates curl coreutils gawk sed' ;;
    pacman) printf '%s\n' 'sudo pacman -S --needed bash ca-certificates curl coreutils gawk sed' ;;
    zypper) printf '%s\n' 'sudo zypper install -y bash ca-certificates curl coreutils gawk sed' ;;
    brew) printf '%s\n' 'brew install bash curl coreutils gawk gnu-sed' ;;
  esac
}

run_as_root() {
  if [ "$EUID" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    echo "✗ Administrator access is required, but sudo was not found" >&2
    return 1
  fi
}

install_dependencies() {
  case "$1" in
    apt-get)
      command -v apt-get >/dev/null 2>&1 || return 1
      run_as_root apt-get update
      run_as_root apt-get install -y bash ca-certificates curl coreutils gawk sed
      ;;
    dnf)
      command -v dnf >/dev/null 2>&1 || return 1
      run_as_root dnf install -y bash ca-certificates curl coreutils gawk sed
      ;;
    yum)
      command -v yum >/dev/null 2>&1 || return 1
      run_as_root yum install -y bash ca-certificates curl coreutils gawk sed
      ;;
    apk)
      command -v apk >/dev/null 2>&1 || return 1
      run_as_root apk add --no-cache bash ca-certificates curl coreutils gawk sed
      ;;
    pacman)
      command -v pacman >/dev/null 2>&1 || return 1
      run_as_root pacman -S --needed bash ca-certificates curl coreutils gawk sed
      ;;
    zypper)
      command -v zypper >/dev/null 2>&1 || return 1
      run_as_root zypper install -y bash ca-certificates curl coreutils gawk sed
      ;;
    brew)
      command -v brew >/dev/null 2>&1 || return 1
      brew install bash curl coreutils gawk gnu-sed
      ;;
    *) return 1 ;;
  esac
}

collect_missing_dependencies
if [ "${#MISSING_COMMANDS[@]}" -gt 0 ]; then
  MANAGER=$(dependency_manager)
  echo "✗ Missing required tools: ${MISSING_COMMANDS[*]}" >&2
  if [ -n "$MANAGER" ]; then
    echo "  Install them with:" >&2
    echo "    $(dependency_install_hint "$MANAGER")" >&2
  else
    echo "  Install these tools, then run the installer again." >&2
  fi
  if [ "$INSTALL_DEPS" != "1" ]; then
    echo "  Or rerun with --install-deps to install them after confirmation." >&2
    exit 1
  fi
  if [ -z "$MANAGER" ]; then
    echo "✗ No supported package manager was found; install the tools manually" >&2
    exit 1
  fi
  echo "  Ready to install the missing tools." >&2
  if ! { printf '  Continue? [y/N] ' >/dev/tty && IFS= read -r REPLY </dev/tty; } 2>/dev/null; then
    echo "✗ Cannot ask for confirmation here; stopped without making changes" >&2
    exit 1
  fi
  case "$REPLY" in
    y|Y|yes|YES) ;;
    *) echo "Cancelled; no changes were made" >&2; exit 1 ;;
  esac
  if ! install_dependencies "$MANAGER"; then
    echo "✗ Dependency installation failed; run the command above manually" >&2
    exit 1
  fi
  collect_missing_dependencies
  if [ "${#MISSING_COMMANDS[@]}" -gt 0 ]; then
    echo "✗ Still missing after installation: ${MISSING_COMMANDS[*]}" >&2
    exit 1
  fi
  echo "✓ Required tools are ready"
fi

GITHUB="${GITHUB:-https://github.com}"
GITHUB_API="${GITHUB_API:-https://api.github.com}"
REPO="${REPO:-Neo-Isshin/MuxLM}"
if [ -z "${BINDIR:-}" ] && [ -z "${HOME:-}" ]; then
  echo "✗ No default install location; set HOME or BINDIR" >&2
  exit 1
fi
BINDIR="${BINDIR:-$HOME/.local/bin}"
FORCE="${FORCE:-0}"

case "$(uname -s)/$(uname -m)" in
  Darwin/arm64)  GOOS=darwin; GOARCH=arm64 ;;
  Darwin/x86_64) GOOS=darwin; GOARCH=amd64 ;;
  Linux/arm64|Linux/aarch64) GOOS=linux; GOARCH=arm64 ;;
  Linux/x86_64)  GOOS=linux;  GOARCH=amd64 ;;
  *) echo "✗ Unsupported platform: $(uname -s)/$(uname -m)" >&2; exit 1 ;;
esac

# Resolve the latest tag first so the binary and SHA256SUMS come from one release.
RELEASE_API="$GITHUB_API/repos/$REPO/releases/latest"
TAG=$(curl -fsSL -H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2022-11-28' "$RELEASE_API" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
[ -n "$TAG" ] || { echo "✗ Could not find the latest release; try again later" >&2; exit 1; }

ASSET="muxlm-$GOOS-$GOARCH"
LEGACY_ASSET="providerdeck-$GOOS-$GOARCH"
URL="$GITHUB/$REPO/releases/download/$TAG/$ASSET"
SUMS_URL="$GITHUB/$REPO/releases/download/$TAG/SHA256SUMS"

mkdir -p "$BINDIR"

if command -v shasum >/dev/null 2>&1; then
  sha256_file() { shasum -a 256 "$1" | awk '{print $1}'; }
elif command -v sha256sum >/dev/null 2>&1; then
  sha256_file() { sha256sum "$1" | awk '{print $1}'; }
else
  echo "✗ shasum or sha256sum is required to verify the download" >&2
  exit 1
fi

BIN="$BINDIR/muxlm"
MARKER="$BINDIR/.muxlm-install.sha256"
PROVIDERDECK_BIN="$BINDIR/providerdeck"
PROVIDERDECK_MARKER="$BINDIR/.providerdeck-install.sha256"

validate_marker_target() {
	local marker="$1"
	if [ -L "$marker" ] || [ -d "$marker" ] || { [ -e "$marker" ] && [ ! -f "$marker" ]; }; then
		echo "✗ Unsafe install marker; stopped: $marker" >&2
		exit 1
	fi
}

managed_binary() {
	local bin="$1"
	local marker="$2"
	local marked_sha=""
	[ -f "$bin" ] && [ -f "$marker" ] || return 1
	IFS= read -r marked_sha < "$marker" || true
	case "$marked_sha" in
		""|*[!0-9a-fA-F]*) return 1 ;;
	esac
	[ "${#marked_sha}" -eq 64 ] || return 1
	[ "$(sha256_file "$bin")" = "$marked_sha" ]
}

authorize_binary_target() {
	validate_marker_target "$MARKER"
	if [ -L "$BIN" ]; then
    if [ "$FORCE" != "1" ]; then
      echo "✗ $BIN is already used by another link; nothing was changed" >&2
      echo "  Use FORCE=1 only if you intend to replace it" >&2
      exit 1
    fi
    return
  fi
  if [ -d "$BIN" ]; then
    echo "✗ $BIN is a directory; nothing was changed" >&2
    exit 1
  fi
  if [ -e "$BIN" ] && [ ! -f "$BIN" ]; then
    echo "✗ $BIN is not a replaceable file; nothing was changed" >&2
    exit 1
  fi
	if [ -f "$BIN" ] && ! managed_binary "$BIN" "$MARKER" && [ "$FORCE" != "1" ]; then
		echo "✗ $BIN is unmanaged or has been modified; nothing was changed" >&2
    echo "  Move it first, or use FORCE=1 only if you intend to replace it" >&2
    exit 1
  fi
}

authorize_providerdeck_target() {
	validate_marker_target "$PROVIDERDECK_MARKER"
}

# Existing links made by this installer are safe to reuse. Other ordinary files
# require FORCE=1; real directories and special files are never replaced.
validate_entry_targets() {
  for ENTRY in cdx cld opc; do
    DEST="$BINDIR/$ENTRY"
    if [ -L "$DEST" ]; then
      TARGET=$(readlink "$DEST")
      case "${TARGET##*/}" in
		muxlm|providerdeck|ez-switch) continue ;;
      esac
      if [ "$FORCE" != "1" ]; then
        echo "✗ $DEST is already used by another command; nothing was changed" >&2
        echo "  Move it first, or use FORCE=1 only if you intend to replace it" >&2
        exit 1
      fi
      continue
    fi
    if [ -d "$DEST" ]; then
      echo "✗ $DEST is a directory; nothing was changed" >&2
      exit 1
    fi
    if [ -e "$DEST" ] && [ ! -f "$DEST" ]; then
      echo "✗ $DEST is not a replaceable file; nothing was changed" >&2
      exit 1
    fi
    if [ -f "$DEST" ] && [ "$FORCE" != "1" ]; then
      echo "✗ $DEST already exists; nothing was changed" >&2
      echo "  Move it first, or use FORCE=1 only if you intend to replace it" >&2
      exit 1
    fi
  done
}

prepare_entry_targets() {
	validate_entry_targets
	authorize_providerdeck_target
	for ENTRY in cdx cld opc; do
    DEST="$BINDIR/$ENTRY"
    if [ -L "$DEST" ] || [ -f "$DEST" ]; then
      rm -f "$DEST"
    fi
		done
}

prepare_providerdeck_compat() {
	if [ -L "$PROVIDERDECK_BIN" ]; then
		local target
		target=$(readlink "$PROVIDERDECK_BIN")
		case "${target##*/}" in
			muxlm|providerdeck|ez-switch) rm -f "$PROVIDERDECK_BIN" ;;
			*)
				echo "  ⚠ Kept existing $PROVIDERDECK_BIN; ProviderDeck command was not created" >&2
				return
				;;
		esac
	elif [ -d "$PROVIDERDECK_BIN" ] || { [ -e "$PROVIDERDECK_BIN" ] && [ ! -f "$PROVIDERDECK_BIN" ]; }; then
		echo "  ⚠ Kept existing $PROVIDERDECK_BIN; ProviderDeck command was not created" >&2
		return
	elif [ -f "$PROVIDERDECK_BIN" ]; then
		if managed_binary "$PROVIDERDECK_BIN" "$PROVIDERDECK_MARKER"; then
			rm -f "$PROVIDERDECK_BIN" "$PROVIDERDECK_MARKER"
		else
			echo "  ⚠ Kept existing $PROVIDERDECK_BIN; ProviderDeck command was not created" >&2
			return
		fi
	fi
	rm -f "$PROVIDERDECK_MARKER"
	( cd "$BINDIR" && ln -s muxlm providerdeck )
}

prepare_ez_switch_compat() {
	local dest="$BINDIR/ez-switch"
	if [ -L "$dest" ]; then
		local target
		target=$(readlink "$dest")
		case "${target##*/}" in
			muxlm|providerdeck|ez-switch) rm -f "$dest" ;;
			*)
				if [ "$FORCE" = "1" ]; then rm -f "$dest"; else
					echo "  ⚠ Kept existing $dest; ez-switch command was not created" >&2
					return
				fi
				;;
		esac
	elif [ -d "$dest" ] || { [ -e "$dest" ] && [ ! -f "$dest" ]; }; then
		echo "  ⚠ Kept existing $dest; ez-switch command was not created" >&2
		return
	elif [ -f "$dest" ]; then
		if [ "$FORCE" = "1" ]; then rm -f "$dest"; else
			echo "  ⚠ Kept existing $dest; ez-switch command was not created" >&2
			return
		fi
	fi
	( cd "$BINDIR" && ln -s muxlm ez-switch )
}

authorize_binary_target
authorize_providerdeck_target
validate_entry_targets

echo "→ Installing MuxLM $TAG ($GOOS/$GOARCH)"
TMP_BIN=$(mktemp "$BINDIR/.muxlm.XXXXXX")
TMP_SUMS=$(mktemp "$BINDIR/.muxlm-sums.XXXXXX")
TMP_MARKER=$(mktemp "$BINDIR/.muxlm-marker.XXXXXX")
cleanup() { rm -f "$TMP_BIN" "$TMP_SUMS" "$TMP_MARKER"; }
trap cleanup EXIT

if ! curl -fsSL "$URL" -o "$TMP_BIN"; then
	ASSET="$LEGACY_ASSET"
	URL="$GITHUB/$REPO/releases/download/$TAG/$ASSET"
	echo "  ↳ Primary release asset unavailable; trying $ASSET" >&2
	curl -fsSL "$URL" -o "$TMP_BIN"
fi
curl -fsSL "$SUMS_URL" -o "$TMP_SUMS"
EXPECTED=$(awk -v asset="$ASSET" '$2 == asset { print $1; exit }' "$TMP_SUMS")
[ -n "$EXPECTED" ] || { echo "✗ $ASSET is missing from SHA256SUMS" >&2; exit 1; }
ACTUAL=$(sha256_file "$TMP_BIN")
[ "$ACTUAL" = "$EXPECTED" ] || { echo "✗ Download verification failed" >&2; exit 1; }

chmod 755 "$TMP_BIN"
printf '%s\n' "$ACTUAL" > "$TMP_MARKER"
chmod 600 "$TMP_MARKER"

# Re-check after downloads so a target changed during installation is not
# silently replaced. FORCE may replace a symlink, but never a directory or
# another special file.
authorize_binary_target
prepare_entry_targets
if [ -L "$BIN" ]; then
  rm -f "$BIN"
fi
mv -f "$TMP_BIN" "$BIN"
( cd "$BINDIR" && ln -s muxlm cdx && ln -s muxlm cld && ln -s muxlm opc )
prepare_providerdeck_compat
prepare_ez_switch_compat

# Publish the marker without following an existing link or treating a directory
# as a rename destination. A hard link from the private temp file also fails if
# another path appears between the checks.
validate_marker_target "$MARKER"
if [ -e "$MARKER" ]; then
  rm -f "$MARKER"
fi
ln "$TMP_MARKER" "$MARKER"
rm -f "$TMP_MARKER"
rm -f "$TMP_SUMS"
trap - EXIT

print_detected_tool() {
  local label="$1"
  local command_name="$2"
  local tool_path=""
  if tool_path=$(command -v "$command_name" 2>/dev/null); then
    printf '  %-12s %s\n' "$label" "$tool_path"
  else
    printf '  %-12s %s\n' "$label" "not found"
  fi
}

echo "✓ Download verified"
echo "✓ Installed to ${BINDIR}"
echo
echo "Detected tools:"
print_detected_tool "Claude Code" "claude"
print_detected_tool "Codex" "codex"
print_detected_tool "OpenCode" "opencode"
echo
echo "MuxLM commands:"
for ENTRY in muxlm cdx cld opc; do
  printf '  %-12s %s\n' "$ENTRY" "$BINDIR/$ENTRY"
done
case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *)
    printf -v QUOTED_BINDIR '%q' "$BINDIR"
    echo
    echo "⚠ This shell cannot find cld yet. Run:"
    echo "    export PATH=$QUOTED_BINDIR:\$PATH"
    ;;
esac
