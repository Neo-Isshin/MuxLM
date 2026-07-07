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
echo "→ $URL  ($TAG)"

mkdir -p "$BINDIR"
curl -fsSL "$URL" -o "$BINDIR/ez-switch"
chmod +x "$BINDIR/ez-switch"
( cd "$BINDIR" && ln -sf ez-switch cdx && ln -sf ez-switch cld && ln -sf ez-switch opc )

echo "✓ $BINDIR/{ez-switch,cdx,cld,opc} 已就绪"
case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) echo "  ⚠ $BINDIR 不在 PATH：export PATH=\"\$HOME/.local/bin:\$PATH\"" ;;
esac
