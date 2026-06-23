#!/usr/bin/env bash
# ez-switch 一键安装：  curl -fsSL https://gitea.nxc8335.cloud/nxc8335/ez-switch/raw/branch/main/install.sh | bash
# （私有仓库，克隆/下载需在 URL 里带 token。）
set -euo pipefail

GITEA="${GITEA:-https://gitea.nxc8335.cloud}"
REPO="${REPO:-nxc8335/ez-switch}"
BINDIR="${BINDIR:-$HOME/.local/bin}"
VERSION="${VERSION:-latest}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"   # darwin / linux
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "不支持的架构: $ARCH"; exit 1 ;;
esac

echo "安装 ez-switch $VERSION ($OS/$ARCH) → $BINDIR"
mkdir -p "$BINDIR"

# 优先用预编译 release 资产（需已发布）；否则回退源码构建（需要 Go + 仓库访问权限）
URL="$GITEA/$REPO/releases/download/$VERSION/ez-switch-${OS}-${ARCH}"
if curl -fsSL "$URL" -o "$BINDIR/ez-switch"; then
  chmod +x "$BINDIR/ez-switch"
else
  echo "⚠️  没找到预编译二进制，回退源码构建（需要 Go + 仓库访问权限）..."
  if ! command -v go >/dev/null 2>&1; then
    echo "❌ 需要 Go；请先安装 Go 或发布预编译二进制"; exit 1
  fi
  tmp="$(mktemp -d)"
  git clone --depth 1 "$GITEA/$REPO" "$tmp/repo" || { echo "❌ 克隆失败（私有仓库需在 URL 带 token）"; exit 1; }
  ( cd "$tmp/repo" && go build -o "$BINDIR/ez-switch" . ) || { echo "❌ 构建失败"; exit 1; }
  chmod +x "$BINDIR/ez-switch"
  rm -rf "$tmp"
fi

# 三个入口：cdx→codex, cld→claude, opc→opencode（软链到同一二进制）
( cd "$BINDIR" && ln -sf ez-switch cdx && ln -sf ez-switch cld && ln -sf ez-switch opc )
echo "✅ 已安装: ez-switch  (入口: cdx / cld / opc)"

case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) echo "⚠️  $BINDIR 不在 PATH 中，请把它加进你的 shell 配置" ;;
esac

echo ""
echo "入口名 cdx/cld/opc 已避开常见系统命令（cc=编译器、oc=OpenShift 等均不冲突）。"

"$BINDIR/cld" list >/dev/null 2>&1 || true
