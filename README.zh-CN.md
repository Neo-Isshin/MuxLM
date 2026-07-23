# MuxLM

用一条短命令，为 Codex、Claude Code 或 OpenCode 选择 provider 和模型。

[English](README.md)

MuxLM 是轻量 CLI 切换器，不是代理服务。底层 CLI 会直连所选 provider；启动时使用的临时配置也不会污染原有全局配置。

## 主要优势

- 一个二进制，三个入口：`cdx`、`cld`、`opc`
- Provider/model catalog 可独立更新，离线时仍有缓存和内置版本兜底
- Catalog 更新既能新增 provider/model，也能删除退役模型并切换 `latest`
- API key 优先保存在 macOS Keychain 或 Linux Secret Service
- 支持具名 key、国内/海外线路和自定义 provider
- 无守护进程、无数据库、无 GUI、无协议代理

## 安装

预编译版本支持 macOS、Linux 的 ARM64 和 AMD64。你还需要先安装准备使用的底层 CLI，并确保它在 `PATH` 中。

```bash
curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash
```

安装器会校验 release checksum，把 `muxlm` 安装到 `~/.local/bin`，并创建 `cdx`、`cld`、`opc` 三个命令。如果提示该目录不在 `PATH`，按屏幕给出的命令添加即可。

安装器会在下载前统一检查依赖，并针对 apt、dnf、yum、apk、pacman、zypper 或 Homebrew 显示对应的安装命令。也可以让安装器在确认后执行该命令：

```bash
curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash -s -- --install-deps
```

它不会静默运行 `sudo`；执行系统包管理器前必须在终端确认。由于最外层命令本身需要 `curl` 和 `bash`，如果其中任何一个尚未安装，请先使用系统包管理器安装，再运行上面的 one-liner。

## Linux 使用指南

### 支持范围

MuxLM 提供 Linux AMD64（`x86_64`）和 ARM64（`aarch64`/`arm64`）静态二进制，主要覆盖 Debian、Ubuntu 和 Fedora。Arch 等常见发行版通常也可直接运行；Alpine 列为尽力兼容，因为 Codex、Claude Code、OpenCode 及其 Node.js 依赖未必完整支持 musl。

MuxLM 本身不要求在目标机器安装 Go 或 Git。通过上面的命令安装和自更新时需要：

- `bash`
- `curl`
- `sha256sum` 或 `shasum`
- 基础 Unix 命令，包括 `awk`、`sed`、`mktemp` 和 `readlink`

Debian/Ubuntu 和 Fedora 的 `coreutils` 包都提供 `sha256sum`。缺少依赖时，安装器会一次列出所有缺失项和可执行的包管理器命令；`--install-deps` 只补齐 MuxLM 安装所需的系统依赖，不会安装 Codex、Claude Code 或 OpenCode。

### 把用户命令目录加入 PATH

默认安装目录是 `~/.local/bin`。如果安装完成后出现 `cld: command not found`，先在当前终端执行：

```bash
export PATH="$HOME/.local/bin:$PATH"
cld doctor
```

Bash 或 Zsh 用户可以把同一条 `export` 命令加入 `~/.bashrc` 或 `~/.zshrc`；Fish 用户可以运行 `fish_add_path ~/.local/bin`。重新打开终端后，可用下面的命令确认：

```bash
command -v cdx cld opc
```

`cld doctor` 会检查 Linux 安装依赖、实际密钥后端、用户命令目录以及三个 AI 工具是否能从 `PATH` 找到。它只读取本地路径和配置元数据，不读取 API key、不连接密钥服务，也不发起网络请求。

### 桌面 Linux 与无桌面服务器

有桌面会话时，MuxLM 会在 `secret-tool` 和会话 D-Bus 可用时选择 Secret Service。Debian/Ubuntu 可安装提供 `secret-tool` 的 `libsecret-tools`，Fedora 对应 `libsecret`；还需要登录会话中的 GNOME Keyring、KWallet 等 Secret Service 服务处于可用状态。

VPS、NAS 和其它无桌面 Linux 通常没有会话 D-Bus。建议在使用 MuxLM 前显式选择本地文件后端：

```bash
export MUXLM_SECRET_BACKEND=file
cld doctor
```

需要长期使用时，把这条 `export` 加入登录 shell 配置。文件后端将密钥以明文保存在 MuxLM 配置目录内的私有文件中，并强制使用 `0600` 权限；不要同步、共享或提交该目录。

配置目录的选择顺序是：

1. `MUXLM_CONFIG_DIR`
2. `PROVIDERDECK_CONFIG_DIR`
3. `CX_CONFIG_DIR`
4. Linux 上的 `$XDG_CONFIG_HOME/muxlm`
5. `~/.config/muxlm`

已有 `~/.config/muxlm`、ProviderDeck 或 ez-switch/cx 配置仍会按兼容规则读取，不需要手工搬迁。

### Linux 上的工具更新边界

```bash
cld update --tool
```

这条命令会找到 `PATH` 中已有的 Codex、Claude Code 和 OpenCode，并调用各自公开的 `update`/`upgrade` 命令。安装来源的识别和升级由底层工具完成；MuxLM 不会自行运行 `apt`、`dnf`、`pacman`、AUR helper 或 Nix，也不会把工具切换到另一种安装来源。

如果某个工具由发行版包、只读系统目录或管理员统一部署，其自带更新可能拒绝操作。这时其余工具仍会继续更新；失败的工具请使用原来的包管理器或联系管理员升级。`cld update --self` 也只更新由本文安装器管理的 MuxLM，不会覆盖系统包或手工放置的二进制。

### Linux 故障排查

- `cld` 找不到：把 `~/.local/bin` 加入 `PATH`，然后重新打开终端。
- `doctor` 显示 `codex`、`claude` 或 `opencode` 未找到：安装你实际要使用的底层 CLI，并确认其命令在 `PATH` 中；未使用的工具可以忽略。
- Secret Service 保存失败：桌面环境检查 `secret-tool`、会话 D-Bus 和已解锁的密钥环；无桌面服务器设置 `MUXLM_SECRET_BACKEND=file`。
- `update --tool` 中某个工具失败：使用该工具原来的 npm、Linuxbrew、系统包或管理员部署方式升级。
- `update --self` 拒绝覆盖：当前二进制不受 MuxLM 安装器管理，请沿用原安装方式。
- 仍无法判断时：运行 `cld doctor`，按 `warning` 后的建议处理；该命令不会修改系统。

## 首次使用

先确认 MuxLM 能找到你准备使用的 AI 工具：

```bash
cld doctor
```

`codex`、`claude` 或 `opencode` 中只要安装了你需要的那个即可；没有使用的工具显示 `not found` 不影响其它入口。接着更新模型列表并查看可用别名：

```bash
cld update
cld list
```

然后选择一个 provider 启动。第一次使用时，MuxLM 会提示你输入 API key，输入内容不会显示在屏幕上；验证通过后会安全保存：

```bash
cld k                      # Claude Code + 按量计费 Kimi 最新模型
cld k27                    # Claude Code + 按量计费 Kimi K2.7 Code
cld kc                     # Claude Code + Kimi Coding Plan
cdx glm                    # Codex + 最新 GLM
opc ds                     # OpenCode + 最新 DeepSeek
```

Kimi 的按量计费 API 使用 `k`，并可通过 `k27`、`k26` 固定模型；`kc` 使用 Coding Plan，固定调用其唯一 Model ID `kimi-for-coding`。两种产品的 API key 不通用。旧的 `kimi`、`kimic`、`kimi26` 和 `k3` 已退役，避免把原来指向 Coding Plan 的命令静默改成按量计费。

使用 `cld k27` 时，Kimi K2.7 要求 Claude Code 开启 Thinking；进入 Claude Code 后按 `Tab`，确认界面显示 `Thinking on`。MuxLM 会自动配置 Kimi 官方的 Anthropic 端点、256K 压缩窗口以及后台任务模型。

`cdx`、`cld`、`opc` 都能执行 `list`、`doctor`、`config` 和 `update` 等管理命令，任选一个入口即可。

## 快速开始

选择一个入口命令，再加 provider 别名：

```bash
cld k27                    # Claude Code + 按量计费 Kimi K2.7 Code
cld kc                     # Claude Code + Kimi Coding Plan
cld glm                    # Claude Code + 最新 GLM
cdx m --intl               # Codex + MiniMax 海外线路
opc ds                     # OpenCode + 最新 DeepSeek
```

最常用的三个选项：

```bash
opc ds -m deepseek-v4-pro  # 临时指定模型 ID
cld glm -- "fix the bug"   # 把 -- 后的参数原样传给底层 CLI
cdx glm --dry-run          # 只预览配置，不实际启动
```

`cdx` 启动 Codex，`cld` 启动 Claude Code，`opc` 启动 OpenCode。兼容性由 catalog 决定；MuxLM 不做协议转换。

## 常用命令

```text
<入口> list                 查看 provider、别名和模型
<入口> config               查看和管理 provider 与 key
<入口> add                  添加 provider key 或自定义 provider
<入口> set-key <别名>       增加一把具名 key
<入口> remove <别名>        删除本地 provider 配置
<入口> update               更新模型列表
<入口> update --tool        更新已检测到的 Codex、Claude Code、OpenCode
<入口> update --self        更新 MuxLM
<入口> update --all         全部更新
<入口> doctor               执行本地只读诊断
<入口> version              显示程序和 catalog 版本
<入口> --help               显示完整帮助
```

## Catalog 自动更新

每次正常启动都会检查 catalog。合法更新会原子写入，并可立即用于当前命令；检查失败时继续使用上次有效缓存或二进制内置 catalog。

Catalog 更新并非只有“新增”：新 revision 可以增加 provider/model，也可以退役并删除旧模型或别名，以及移动 `latest`。永久 tombstone 会阻止已退役的版本别名被重新使用；严格校验还会拦截回滚、同 revision 篡改和 provider 信任字段的静默变化。

正常启动时发现新版程序只会提示，不会静默替换二进制。

```bash
MUXLM_AUTO_UPDATE=0 cld glm       # 关闭启动检查
MUXLM_UPDATE_DEBUG=1 cld glm      # 显示更新诊断
cld update                        # 立即更新模型列表
```

## 一键更新三个 AI 工具

不需要分别查找三个工具的升级命令，一条命令即可更新电脑里已检测到的 Codex、Claude Code 和 OpenCode：

```bash
cld update --tool
```

这里也可以换成 `cdx update --tool` 或 `opc update --tool`，效果相同。

MuxLM 会在 `PATH` 中找到这三个工具，再交给各自的官方更新命令处理。工具会识别自己来自 npm、Homebrew 还是官方安装程序，并沿用原来的安装渠道，不会把你的安装方式换掉。未安装的工具会自动跳过；其中一个更新失败时，其余工具仍会继续。

如果某个工具版本太旧、还不支持安全的自动更新，MuxLM 会提示你先按原来的安装方式升级一次，而不会误启动它的交互界面。

## 更新模型列表和 MuxLM

这四种用法各有明确含义：

```bash
cld update           # 只更新模型列表
cld update --tool    # 更新已安装的三个 AI 工具
cld update --self    # 只更新 MuxLM
cld update --all     # 依次完成以上全部更新
```

通过本文安装命令安装的 MuxLM 可以自动更新。若当前副本来自其它地方，程序会停止并提示沿用原来的安装方式，不会贸然覆盖文件。

## 使用自己的 Catalog 服务器

把 `catalog.json` 放在静态 HTTPS 地址，建议支持 `ETag` 或 `Last-Modified`，然后设置：

```bash
export MUXLM_CATALOG_URL=https://example.com/catalog.json
```

在迁移到你的服务器之前，默认 catalog 暂时由本 GitHub 仓库提供。下载上限为 2 MiB，并经过严格 schema、revision 单调且不可变、回滚保护、tombstone 和信任字段校验。

## 隐私与旧版迁移

- 子进程只获得当前 provider 的 key，其它 provider key 会从环境中清理。
- Codex 和 OpenCode 使用一次性配置目录。
- 配置目录依次采用 `MUXLM_CONFIG_DIR`、`PROVIDERDECK_CONFIG_DIR`、`CX_CONFIG_DIR`；均未设置时，Linux 使用 `$XDG_CONFIG_HOME/muxlm` 或 `~/.config/muxlm`，macOS 默认使用 `~/.config/muxlm`。已有 ProviderDeck 和 ez-switch/cx 的配置及密钥仍可读取，不做破坏性迁移。
- 环境变量优先级为 `MUXLM_*`、`PROVIDERDECK_*`、`CX_*`。
- 安装器会在确认安全时保留 `providerdeck`、`ez-switch` 兼容命令。

## 从源码构建

```bash
go test ./...
go build -ldflags "-X main.appVersion=v2.2.0" -o muxlm .
```

采用 [MIT License](LICENSE)。种子 catalog 含有社区来源数据，详见[第三方声明](THIRD_PARTY_NOTICES.md)。
