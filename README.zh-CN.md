# ProviderDeck

一条短命令，为 Codex、Claude Code 或 OpenCode 选择 provider 和模型。

[English](README.md)

ProviderDeck 是轻量 CLI 切换器，不是代理服务。底层 CLI 会直连所选 provider，也不会污染你原有的全局配置。

## 主要优势

- 一个二进制，三个入口：`cdx`、`cld`、`opc`
- Provider/model catalog 可独立更新，离线时有内置版本兜底
- 裸别名跟随 `latest`；已发布的版本别名固定具体模型
- Catalog 可新增模型、删除退役模型和别名、切换 `latest`
- API key 优先保存在 macOS Keychain 或 Linux Secret Service
- Codex/OpenCode 使用一次性配置，不污染全局设置
- 支持多把具名 key、国内/海外线路和自定义 provider
- 无守护进程、无数据库、无 GUI、无协议代理

## 安装

预编译版本支持 macOS、Linux 的 ARM64 和 AMD64。

```bash
curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/ProviderDeck/main/install.sh | bash
```

安装器会校验 `SHA256SUMS`，把 `providerdeck` 安装到 `~/.local/bin`，并创建 `cdx`、`cld`、`opc` 三个入口。已有的无关命令不会被覆盖，除非显式设置 `FORCE=1`。你还需要把准备使用的底层 CLI 安装到 `PATH`。

## 快速开始

```bash
cld glm                           # Claude Code + 最新 GLM
cdx m --intl                      # Codex + MiniMax 海外线路
opc ds                            # OpenCode + 最新 DeepSeek
opc ds -m deepseek-v4-pro         # 临时覆盖模型 ID
cld glm -- "fix the bug"          # 参数透传给 Claude Code
cdx glm --dry-run                 # 只预览，不启动
cdx doctor                        # 本地只读诊断
```

缺少 key 时，ProviderDeck 会隐藏输入、验证一次并保存，后续可直接使用。

| 入口 | 启动目标 | 线路 |
|---|---|---|
| `cdx <别名>` | Codex | OpenAI-compatible |
| `cld <别名>` | Claude Code | Anthropic-compatible |
| `opc <别名>` | OpenCode | 选择可用线路 |

兼容性由 catalog 决定；ProviderDeck 不做协议转换。

## 基本命令

```text
<入口> list                 查看 provider、别名和模型
<入口> config               查看/管理 provider 与 key
<入口> add                  添加 provider key 或自定义 provider
<入口> set-key <别名>       增加一把具名 key
<入口> remove <别名>        删除本地 provider 配置
<入口> update               更新 catalog 并检查程序版本
<入口> doctor               执行本地只读诊断
<入口> version              显示程序和 catalog 版本
<入口> --help               显示完整帮助
```

## Catalog 自动更新

每次正常启动都会检查 catalog。程序版本检查单独限制为每小时一次，以免触发 GitHub API 限额；两项同时到期时会并行执行，共用 2.5 秒超时。合法 catalog 会原子写入并可在本次命令中生效。失败时继续使用上次有效缓存或二进制内置 catalog。

Catalog revision 单调递增且内容不可变，格式为 `YYYY-MM-DD.N`。更新可以新增模型/provider、删除退役模型及其版本别名，也可以移动 `latest`；仍然保留的版本别名不能改指另一模型。已有 provider identity、端点、key identity、CLI 能力和 wire protocol 不能被 catalog 静默修改，这类信任边界变化必须跟随经过审查的二进制版本发布。

删除带版本别名的模型时，要把别名和原目标保留在顶层 `retired_tags` 中。Tombstone 永久保留，避免全新安装或清空配置后复用旧别名。

发现新版程序时只提示安装命令，不会静默替换二进制。

```bash
PROVIDERDECK_AUTO_UPDATE=0 cld glm       # 关闭启动检查
PROVIDERDECK_UPDATE_INTERVAL=1h cld glm  # 限制 catalog 检查频率
PROVIDERDECK_RELEASE_INTERVAL=0 cld glm  # 每次启动都检查程序版本
PROVIDERDECK_UPDATE_DEBUG=1 cld glm      # 显示诊断
cld update                               # 立即强制检查
```

### 使用自己的 Catalog 服务器

把 `catalog.json` 放在静态 HTTPS 地址，建议返回 `ETag` 或 `Last-Modified`：

```bash
export PROVIDERDECK_CATALOG_URL=https://example.com/catalog.json
```

下载限制为 2 MiB，并经过严格 schema、回滚保护、revision 不可变和信任字段校验。程序 release API 和安装脚本地址也可分别通过 `PROVIDERDECK_RELEASE_API_URL`、`PROVIDERDECK_INSTALL_URL` 迁移。

## Key、隐私与旧版迁移

- 子进程只获得当前 provider 的 key，其它 provider key 会被清理。
- Codex 使用临时 `CODEX_HOME`；OpenCode 使用临时 `OPENCODE_CONFIG_DIR`。
- 全新安装使用 `~/.config/providerdeck`，目录/文件权限收紧，写入采用原子替换。已有安装继续安全复用 `~/.config/cx`；两目录同时存在时，新目录优先，缺失文件逐项回退旧目录。
- 已有 `CX_*` 环境变量和 `ez-switch` Keychain/Secret Service 记录仍可读取，迁移过程不破坏旧数据；两套环境变量同时存在时以 `PROVIDERDECK_*` 为准。
- 没有系统密钥服务时，会明确提示并回退到权限为 `0600` 的本地文件。

## 从源码构建

```bash
go test ./...
go build -ldflags "-X main.appVersion=v2.0.0" -o providerdeck .
```

采用 [MIT License](LICENSE)。种子 catalog 含有社区来源数据，详见[第三方声明](THIRD_PARTY_NOTICES.md)。
