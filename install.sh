#!/usr/bin/env bash
# 用法: curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/ProviderDeck/main/install.sh | bash
set -euo pipefail

GITHUB="${GITHUB:-https://github.com}"
GITHUB_API="${GITHUB_API:-https://api.github.com}"
REPO="${REPO:-Neo-Isshin/ProviderDeck}"
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

URL="$GITHUB/$REPO/releases/download/$TAG/providerdeck-$GOOS-$GOARCH"
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

BIN="$BINDIR/providerdeck"
MARKER="$BINDIR/.providerdeck-install.sha256"

validate_marker_target() {
  if [ -L "$MARKER" ]; then
    echo "✗ 安装 marker 是符号链接，拒绝使用: $MARKER" >&2
    exit 1
  fi
  if [ -d "$MARKER" ]; then
    echo "✗ 安装 marker 是目录，拒绝使用: $MARKER" >&2
    exit 1
  fi
  if [ -e "$MARKER" ] && [ ! -f "$MARKER" ]; then
    echo "✗ 安装 marker 不是普通文件，拒绝使用: $MARKER" >&2
    exit 1
  fi
}

managed_binary() {
  [ -f "$MARKER" ] || return 1
  MARKED_SHA=""
  IFS= read -r MARKED_SHA < "$MARKER" || true
  case "$MARKED_SHA" in
    ""|*[!0-9a-fA-F]*) return 1 ;;
  esac
  [ "${#MARKED_SHA}" -eq 64 ] || return 1
  [ "$(sha256_file "$BIN")" = "$MARKED_SHA" ]
}

authorize_binary_target() {
  validate_marker_target
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
  if [ -f "$BIN" ] && ! managed_binary && [ "$FORCE" != "1" ]; then
    echo "✗ $BIN 不是此安装器管理的文件，或内容已变化（未覆盖）" >&2
    echo "  请先移动该文件，或确认后使用 FORCE=1 重新安装" >&2
    exit 1
  fi
}

authorize_binary_target

# Existing links made by this installer are safe to reuse. Other ordinary files
# require FORCE=1; real directories and special files are never replaced.
validate_entry_targets() {
  for ENTRY in cdx cld opc; do
    DEST="$BINDIR/$ENTRY"
    if [ -L "$DEST" ]; then
      TARGET=$(readlink "$DEST")
      case "${TARGET##*/}" in
        providerdeck|ez-switch) continue ;;
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
  for ENTRY in cdx cld opc; do
    DEST="$BINDIR/$ENTRY"
    if [ -L "$DEST" ] || [ -f "$DEST" ]; then
      rm -f "$DEST"
    fi
  done
}

validate_entry_targets

echo "→ 安装 ProviderDeck $TAG ($GOOS/$GOARCH)"
TMP_BIN=$(mktemp "$BINDIR/.providerdeck.XXXXXX")
TMP_SUMS=$(mktemp "$BINDIR/.providerdeck-sums.XXXXXX")
TMP_MARKER=$(mktemp "$BINDIR/.providerdeck-marker.XXXXXX")
cleanup() { rm -f "$TMP_BIN" "$TMP_SUMS" "$TMP_MARKER"; }
trap cleanup EXIT

curl -fsSL "$URL" -o "$TMP_BIN"
curl -fsSL "$SUMS_URL" -o "$TMP_SUMS"
ASSET="providerdeck-$GOOS-$GOARCH"
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
( cd "$BINDIR" && ln -s providerdeck cdx && ln -s providerdeck cld && ln -s providerdeck opc )

# Publish the marker without following an existing link or treating a directory
# as a rename destination. A hard link from the private temp file also fails if
# another path appears between the checks.
validate_marker_target
if [ -e "$MARKER" ]; then
  rm -f "$MARKER"
fi
ln "$TMP_MARKER" "$MARKER"
rm -f "$TMP_MARKER"
rm -f "$TMP_SUMS"
trap - EXIT

echo "✓ SHA-256 校验通过"
echo "✓ 已安装到 ${BINDIR}（cdx / cld / opc）"
case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *)
    printf -v QUOTED_BINDIR '%q' "$BINDIR"
    echo "  ⚠ $BINDIR 不在 PATH；当前 shell 可运行："
    echo "    export PATH=$QUOTED_BINDIR:\$PATH"
    ;;
esac
