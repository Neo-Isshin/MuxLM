#!/usr/bin/env bash
# 用法: curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash
set -euo pipefail

usage() {
  printf '%s\n' \
    'MuxLM 安装器' \
    '' \
    '用法:' \
    '  bash install.sh' \
    '  bash install.sh --install-deps' \
    '  curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash' \
    '  curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash -s -- --install-deps' \
    '' \
    '选项:' \
    '  --install-deps  检测到缺失依赖时，显示命令并在确认后调用系统包管理器' \
    '  -h, --help      显示帮助'
}

INSTALL_DEPS=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-deps) INSTALL_DEPS=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "✗ 未知安装参数: $1" >&2; usage >&2; exit 2 ;;
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
    echo "✗ 安装系统依赖需要 root 权限，但没有找到 sudo" >&2
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
  echo "✗ 缺少安装依赖: ${MISSING_COMMANDS[*]}" >&2
  if [ -n "$MANAGER" ]; then
    echo "  可执行:" >&2
    echo "    $(dependency_install_hint "$MANAGER")" >&2
  else
    echo "  请先使用系统包管理器安装 bash、curl、CA 证书、校验工具和基础 Unix 命令。" >&2
  fi
  if [ "$INSTALL_DEPS" != "1" ]; then
    echo "  也可以重新运行安装器并添加 --install-deps；执行系统安装命令前仍会要求确认。" >&2
    exit 1
  fi
  if [ -z "$MANAGER" ]; then
    echo "✗ 无法识别系统包管理器，不能自动补齐依赖" >&2
    exit 1
  fi
  echo "  MuxLM 准备执行上面的系统安装命令。" >&2
  if ! { printf '  是否继续？[y/N] ' >/dev/tty && IFS= read -r REPLY </dev/tty; } 2>/dev/null; then
    echo "✗ 当前环境无法交互确认；未修改系统软件包" >&2
    exit 1
  fi
  case "$REPLY" in
    y|Y|yes|YES) ;;
    *) echo "已取消；未修改系统软件包" >&2; exit 1 ;;
  esac
  if ! install_dependencies "$MANAGER"; then
    echo "✗ 系统依赖安装失败；请手动执行上面显示的命令" >&2
    exit 1
  fi
  collect_missing_dependencies
  if [ "${#MISSING_COMMANDS[@]}" -gt 0 ]; then
    echo "✗ 安装后仍缺少依赖: ${MISSING_COMMANDS[*]}" >&2
    exit 1
  fi
  echo "✓ 安装依赖已准备好"
fi

GITHUB="${GITHUB:-https://github.com}"
GITHUB_API="${GITHUB_API:-https://api.github.com}"
REPO="${REPO:-Neo-Isshin/MuxLM}"
if [ -z "${BINDIR:-}" ] && [ -z "${HOME:-}" ]; then
  echo "✗ HOME 未设置；请设置 HOME 或通过 BINDIR 指定安装目录" >&2
  exit 1
fi
BINDIR="${BINDIR:-$HOME/.local/bin}"
FORCE="${FORCE:-0}"

case "$(uname -s)/$(uname -m)" in
  Darwin/arm64)  GOOS=darwin; GOARCH=arm64 ;;
  Darwin/x86_64) GOOS=darwin; GOARCH=amd64 ;;
  Linux/arm64|Linux/aarch64) GOOS=linux; GOARCH=arm64 ;;
  Linux/x86_64)  GOOS=linux;  GOARCH=amd64 ;;
  *) echo "✗ 不支持: $(uname -s)/$(uname -m)" >&2; exit 1 ;;
esac

# 先解析 latest tag，确保二进制与 SHA256SUMS 来自同一个 release。
RELEASE_API="$GITHUB_API/repos/$REPO/releases/latest"
TAG=$(curl -fsSL -H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2022-11-28' "$RELEASE_API" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
[ -n "$TAG" ] || { echo "✗ 解析 latest tag 失败: $RELEASE_API" >&2; exit 1; }

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
  echo "✗ 系统缺少 shasum/sha256sum，无法校验下载文件" >&2
  exit 1
fi

BIN="$BINDIR/muxlm"
MARKER="$BINDIR/.muxlm-install.sha256"
PROVIDERDECK_BIN="$BINDIR/providerdeck"
PROVIDERDECK_MARKER="$BINDIR/.providerdeck-install.sha256"

validate_marker_target() {
	local marker="$1"
	if [ -L "$marker" ]; then
		echo "✗ 安装 marker 是符号链接，拒绝使用: $marker" >&2
		exit 1
	fi
	if [ -d "$marker" ]; then
		echo "✗ 安装 marker 是目录，拒绝使用: $marker" >&2
		exit 1
	fi
	if [ -e "$marker" ] && [ ! -f "$marker" ]; then
		echo "✗ 安装 marker 不是普通文件，拒绝使用: $marker" >&2
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
      echo "✗ $BIN 是符号链接（未覆盖）；确认后可使用 FORCE=1" >&2
      exit 1
    fi
    return
  fi
  if [ -d "$BIN" ]; then
    echo "✗ $BIN 是目录（始终不会覆盖）" >&2
    exit 1
  fi
  if [ -e "$BIN" ] && [ ! -f "$BIN" ]; then
    echo "✗ $BIN 不是普通文件（始终不会覆盖）" >&2
    exit 1
  fi
	if [ -f "$BIN" ] && ! managed_binary "$BIN" "$MARKER" && [ "$FORCE" != "1" ]; then
		echo "✗ $BIN 不是此安装器管理的文件，或内容已变化（未覆盖）" >&2
    echo "  请先移动该文件，或确认后使用 FORCE=1 重新安装" >&2
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
        echo "✗ 命令冲突: $DEST 是无关符号链接（未覆盖）" >&2
        echo "  请先移动该链接，或确认后使用 FORCE=1 重新安装" >&2
        exit 1
      fi
      continue
    fi
    if [ -d "$DEST" ]; then
      echo "✗ 命令冲突: $DEST 是目录（始终不会覆盖）" >&2
      exit 1
    fi
    if [ -e "$DEST" ] && [ ! -f "$DEST" ]; then
      echo "✗ 命令冲突: $DEST 不是普通文件（始终不会覆盖）" >&2
      exit 1
    fi
    if [ -f "$DEST" ] && [ "$FORCE" != "1" ]; then
      echo "✗ 命令冲突: $DEST 已存在（未覆盖）" >&2
      echo "  请先移动该文件，或确认后使用 FORCE=1 重新安装" >&2
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
				echo "  ⚠ 保留无关的 $PROVIDERDECK_BIN；未创建 ProviderDeck 兼容链接" >&2
				return
				;;
		esac
	elif [ -d "$PROVIDERDECK_BIN" ] || { [ -e "$PROVIDERDECK_BIN" ] && [ ! -f "$PROVIDERDECK_BIN" ]; }; then
		echo "  ⚠ 保留特殊路径 $PROVIDERDECK_BIN；未创建 ProviderDeck 兼容链接" >&2
		return
	elif [ -f "$PROVIDERDECK_BIN" ]; then
		if managed_binary "$PROVIDERDECK_BIN" "$PROVIDERDECK_MARKER"; then
			rm -f "$PROVIDERDECK_BIN" "$PROVIDERDECK_MARKER"
		else
			echo "  ⚠ 保留未受管的 $PROVIDERDECK_BIN；未创建 ProviderDeck 兼容链接" >&2
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
					echo "  ⚠ 保留无关的 $dest；未创建 ez-switch 兼容链接" >&2
					return
				fi
				;;
		esac
	elif [ -d "$dest" ] || { [ -e "$dest" ] && [ ! -f "$dest" ]; }; then
		echo "  ⚠ 保留特殊路径 $dest；未创建 ez-switch 兼容链接" >&2
		return
	elif [ -f "$dest" ]; then
		if [ "$FORCE" = "1" ]; then rm -f "$dest"; else
			echo "  ⚠ 保留现有 $dest；未创建 ez-switch 兼容链接" >&2
			return
		fi
	fi
	( cd "$BINDIR" && ln -s muxlm ez-switch )
}

authorize_binary_target
authorize_providerdeck_target
validate_entry_targets

echo "→ 安装 MuxLM $TAG ($GOOS/$GOARCH)"
TMP_BIN=$(mktemp "$BINDIR/.muxlm.XXXXXX")
TMP_SUMS=$(mktemp "$BINDIR/.muxlm-sums.XXXXXX")
TMP_MARKER=$(mktemp "$BINDIR/.muxlm-marker.XXXXXX")
cleanup() { rm -f "$TMP_BIN" "$TMP_SUMS" "$TMP_MARKER"; }
trap cleanup EXIT

if ! curl -fsSL "$URL" -o "$TMP_BIN"; then
	ASSET="$LEGACY_ASSET"
	URL="$GITHUB/$REPO/releases/download/$TAG/$ASSET"
	echo "  ↳ canonical asset 暂不可用，回退到兼容资产 $ASSET" >&2
	curl -fsSL "$URL" -o "$TMP_BIN"
fi
curl -fsSL "$SUMS_URL" -o "$TMP_SUMS"
EXPECTED=$(awk -v asset="$ASSET" '$2 == asset { print $1; exit }' "$TMP_SUMS")
[ -n "$EXPECTED" ] || { echo "✗ SHA256SUMS 中没有 $ASSET" >&2; exit 1; }
ACTUAL=$(sha256_file "$TMP_BIN")
[ "$ACTUAL" = "$EXPECTED" ] || { echo "✗ SHA-256 校验失败" >&2; exit 1; }

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

echo "✓ SHA-256 校验通过"
echo "✓ 已安装到 ${BINDIR}（muxlm / cdx / cld / opc）"
case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *)
    printf -v QUOTED_BINDIR '%q' "$BINDIR"
    echo "  ⚠ $BINDIR 不在 PATH；当前 shell 可运行："
    echo "    export PATH=$QUOTED_BINDIR:\$PATH"
    ;;
esac
