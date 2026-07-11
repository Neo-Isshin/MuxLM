#!/usr/bin/env bash
# 用法: curl -fsSL https://gitea.nxc8335.cloud/nxc8335/ez-switch/raw/branch/main/install.sh | bash
set -euo pipefail

GITEA="${GITEA:-https://gitea.nxc8335.cloud}"
REPO="${REPO:-nxc8335/ez-switch}"
BINDIR="${BINDIR:-$HOME/.local/bin}"

case "$(uname -s)/$(uname -m)" in
  Darwin/arm64)  GOOS=darwin; GOARCH=arm64 ;;
  Darwin/x86_64) GOOS=darwin; GOARCH=amd64 ;;
  Linux/arm64)   GOOS=linux;  GOARCH=arm64 ;;
  Linux/x86_64)  GOOS=linux;  GOARCH=amd64 ;;
  *) echo "✗ 不支持: $(uname -s)/$(uname -m)" >&2; exit 1 ;;
esac

# Gitea 的 /releases/latest/download/... 路径不通，必须先解析出 tag 再拼 /releases/download/{tag}/...
TAG=$(curl -fsSL "$GITEA/api/v1/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name":"\([^"]*\)".*/\1/p' | head -1)
[ -n "$TAG" ] || { echo "✗ 解析 latest tag 失败: $GITEA/api/v1/repos/$REPO/releases/latest" >&2; exit 1; }

URL="$GITEA/$REPO/releases/download/$TAG/ez-switch-$GOOS-$GOARCH"
SUMS_URL="$GITEA/$REPO/releases/download/$TAG/SHA256SUMS"
echo "→ $URL  ($TAG)"

mkdir -p "$BINDIR"
TMP_BIN=$(mktemp "$BINDIR/.ez-switch.XXXXXX")
TMP_SUMS=$(mktemp "$BINDIR/.ez-switch-sums.XXXXXX")
cleanup() { rm -f "$TMP_BIN" "$TMP_SUMS"; }
trap cleanup EXIT

curl -fsSL "$URL" -o "$TMP_BIN"
curl -fsSL "$SUMS_URL" -o "$TMP_SUMS"
ASSET="ez-switch-$GOOS-$GOARCH"
EXPECTED=$(awk -v asset="$ASSET" '$2 == asset { print $1; exit }' "$TMP_SUMS")
[ -n "$EXPECTED" ] || { echo "✗ SHA256SUMS 中没有 $ASSET" >&2; exit 1; }
if command -v shasum >/dev/null 2>&1; then
  ACTUAL=$(shasum -a 256 "$TMP_BIN" | awk '{print $1}')
elif command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "$TMP_BIN" | awk '{print $1}')
else
  echo "✗ 系统缺少 shasum/sha256sum，无法校验下载文件" >&2
  exit 1
fi
[ "$ACTUAL" = "$EXPECTED" ] || { echo "✗ SHA-256 校验失败" >&2; exit 1; }

chmod 755 "$TMP_BIN"
mv -f "$TMP_BIN" "$BINDIR/ez-switch"
( cd "$BINDIR" && ln -sf ez-switch cdx && ln -sf ez-switch cld && ln -sf ez-switch opc )
rm -f "$TMP_SUMS"
trap - EXIT

echo "✓ SHA-256 校验通过"
echo "✓ $BINDIR/{ez-switch,cdx,cld,opc} 已就绪"
case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) echo "  ⚠ $BINDIR 不在 PATH：export PATH=\"\$HOME/.local/bin:\$PATH\"" ;;
esac
