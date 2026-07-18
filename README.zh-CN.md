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

## 快速开始

选择一个入口命令，再加 provider 别名：

```bash
cld glm                    # Claude Code + 最新 GLM
cdx m --intl               # Codex + MiniMax 海外线路
opc ds                     # OpenCode + 最新 DeepSeek
```

首次使用某个 provider 时，MuxLM 会隐藏输入 API key，验证成功后安全保存，以后可直接启动。

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
<入口> update               更新 catalog 并检查程序版本
<入口> doctor               执行本地只读诊断
<入口> version              显示程序和 catalog 版本
<入口> --help               显示完整帮助
```

## Catalog 自动更新

每次正常启动都会检查 catalog。合法更新会原子写入，并可立即用于当前命令；检查失败时继续使用上次有效缓存或二进制内置 catalog。

Catalog 更新并非只有“新增”：新 revision 可以增加 provider/model，也可以退役并删除旧模型或别名，以及移动 `latest`。永久 tombstone 会阻止已退役的版本别名被重新使用；严格校验还会拦截回滚、同 revision 篡改和 provider 信任字段的静默变化。

发现新版程序时只会提示，不会静默替换二进制。

```bash
MUXLM_AUTO_UPDATE=0 cld glm       # 关闭启动检查
MUXLM_UPDATE_DEBUG=1 cld glm      # 显示更新诊断
cld update                        # 立即强制检查
```

### 使用自己的 Catalog 服务器

把 `catalog.json` 放在静态 HTTPS 地址，建议支持 `ETag` 或 `Last-Modified`，然后设置：

```bash
export MUXLM_CATALOG_URL=https://example.com/catalog.json
```

在迁移到你的服务器之前，默认 catalog 暂时由本 GitHub 仓库提供。下载上限为 2 MiB，并经过严格 schema、revision 单调且不可变、回滚保护、tombstone 和信任字段校验。

## 隐私与旧版迁移

- 子进程只获得当前 provider 的 key，其它 provider key 会从环境中清理。
- Codex 和 OpenCode 使用一次性配置目录。
- 新配置使用 `~/.config/muxlm`；已有 ProviderDeck 和 ez-switch/cx 的配置及密钥仍可读取，不做破坏性迁移。
- 环境变量优先级为 `MUXLM_*`、`PROVIDERDECK_*`、`CX_*`。
- 安装器会在确认安全时保留 `providerdeck`、`ez-switch` 兼容命令。

## 从源码构建

```bash
go test ./...
go build -ldflags "-X main.appVersion=v2.1.0" -o muxlm .
```

采用 [MIT License](LICENSE)。种子 catalog 含有社区来源数据，详见[第三方声明](THIRD_PARTY_NOTICES.md)。
