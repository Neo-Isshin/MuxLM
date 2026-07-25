# MuxLM

> 在 Codex、Claude Code、OpenCode 里随手切换不同厂商的模型,不用反复改配置,也不用担心 API key 散落各处。

一句话上手:`cdx glm52` —— 立刻用 GLM 5.2 启动 Codex。

[English](README.md) · [Catalog 参照表](CATALOG.zh-CN.md)

MuxLM 是一个**轻量切换器,不是代理**:底层 CLI 直连你选的 provider,启动时使用的临时配置也和你原有的全局配置相互隔离——用完即走,不污染环境。

## 它是什么

| 命令 | 启动谁 | 例子 |
| --- | --- | --- |
| `cdx` | Codex | `cdx glm` → 用最新 GLM 启动 Codex |
| `cld` | Claude Code | `cld k3` → 用 Kimi K3 启动 Claude Code |
| `opc` | OpenCode | `opc ds` → 用最新 DeepSeek 启动 OpenCode |

三个命令背后是**同一个程序**,只是默认启动的 CLI 不同。`cld` 同时也是 MuxLM 的管理入口(`list`、`config`、`update` 等),所以你只需要记住 `cdx / cld / opc` 这三个词。

> 兼容性由 catalog 决定;MuxLM **不做协议转换**。想看某个 provider/model 的完整列表,用 `<入口> list`,或查阅 [Catalog 参照表](CATALOG.zh-CN.md)。

## 主要特性

- 一个二进制,三个入口:`cdx`、`cld`、`opc`
- Catalog(模型/provider 列表)可独立更新,**离线也有缓存和内置版本兜底**
- 既支持厂商官方来源,也支持中转来源,还支持自定义 provider
- API key 优先保存在 macOS Keychain 或 Linux Secret Service
- 支持具名 key、国内/海外线路切换
- 无守护进程、无数据库、无 GUI、无协议代理

## 安装

预编译版本支持 macOS、Linux 的 ARM64 与 AMD64。你还需要先安装准备使用的底层 CLI(Codex / Claude Code / OpenCode),并确保它在 `PATH` 中。

```bash
curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash
```

安装器会:校验 release checksum → 把 `muxlm` 装到 `~/.local/bin` → 创建 `cdx`、`cld`、`opc` 三个命令。如果提示该目录不在 `PATH`,按屏幕给出的命令添加即可。

它会在下载前统一检查依赖,并针对 apt / dnf / yum / apk / pacman / zypper / Homebrew 给出对应命令;也可以让它确认后代为执行:

```bash
curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash -s -- --install-deps
```

安装器**不会**静默运行 `sudo`。最外层命令本身依赖 `curl` 和 `bash`,若其中任何一个尚未安装,请先用系统包管理器装好,再运行上面的 one-liner。

## 30 秒上手

无需任何前置设置,直接选一个模型短名启动:

1. 例如 `opc ds` —— 用 DeepSeek 最新模型启动 OpenCode。
2. **第一次**用某个 provider 时,MuxLM 会提示你输入它的 API key(输入内容不回显),验证通过后安全保存。
3. 之后再用同一个 provider 的任意模型,会自动复用已保存的 key——不必再输。

> 一个 provider 只保存一把 key,该 provider 下所有模型共用这把 key。

## 常用模型速查

**规则只有两条:**

- 只写模型短名 → 走模型厂商的**官方**来源(如 `cld k3`)。
- 想换来源 → 在模型短名前加**来源短名**(如 `opc or k3` 走 OpenRouter)。

| 命令 | 含义 |
| --- | --- |
| `cld def` | Claude Code 原生订阅与默认模型 |
| `cdx def` | Codex 原生账号与默认模型 |
| `opc def` | OpenCode 原生配置与默认模型 |
| `cld k3` | Claude Code + Kimi **官方** K3 |
| `opc or k3` | OpenCode + **OpenRouter** 提供的 Kimi K3 |
| `cld sf k27` | Claude Code + SiliconFlow 提供的 Kimi K2.7 Code |
| `cld k27` | Claude Code + Kimi 按量计费 K2.7 Code |
| `cld kc` | Claude Code + Kimi Coding Plan |
| `cld glm` | Claude Code + 最新 GLM(`cdx glm52` 则固定用 5.2) |
| `cld qc` | Claude Code + 百炼 Coding Plan |
| `cdx q` | Codex + 最新千问 |
| `opc or` | OpenCode + OpenRouter |
| `cdx m --intl` | Codex + 最新 MiniMax M3,走**海外**端点 |
| `opc ds` | OpenCode + 最新 DeepSeek |

**关于 `def`:** 它不使用 MuxLM 的 provider,也不读取已保存的 key,而是清除路由覆盖,让对应 CLI 回到自己的原生账号、配置和默认模型(Claude Code 回到订阅里的 Opus、Fable 等;Codex / OpenCode 回到各自的登录与配置)。需要传原生 CLI 参数时放在 `--` 后,例如 `cld def -- --model opus`。

**同名短名不会冲突:** `cld k3` 永远是 Kimi 官方,`<入口> or k3` 才是 OpenRouter。若你指定的来源当前没有该模型,MuxLM 会直接说明,**不会**悄悄换成该来源的默认模型。原有的 `sfv4f`、`ork3` 等固定版本别名继续可用。

**三个常用选项:**

```bash
opc ds -m deepseek-v4-pro   # 临时指定模型 ID
cld glm -- "fix the bug"    # 把 -- 后的参数原样传给底层 CLI
cdx glm --dry-run           # 只预览配置,不实际启动
```

## 管理命令一览

`cdx`、`cld`、`opc` 任一入口都能执行下面的管理命令(下文统一用 `<入口>` 表示):

| 命令 | 作用 |
| --- | --- |
| `<入口> list` | 查看 provider、别名和模型 |
| `<入口> config` | 查看和管理 provider 与 key |
| `<入口> add` | 添加 provider key 或自定义 provider |
| `<入口> set-key <别名>` | 再加一把具名 key |
| `<入口> remove <别名>` | 删除本地 provider 配置 |
| `<入口> update` | 更新模型列表(详见[更新](#更新)) |
| `<入口> doctor` | 本地只读诊断 |
| `<入口> version` | 显示程序与 catalog 版本 |
| `<入口> --help` | 显示完整帮助 |

## 更新

四种用法,各管一摊:

| 命令 | 更新什么 |
| --- | --- |
| `cld update` | 模型列表 |
| `cld update --tool` | 已安装的 Codex / Claude Code / OpenCode |
| `cld update --self` | MuxLM 本体 |
| `cld update --all` | 上面三项依次全做 |

> 把 `cld` 换成 `cdx` 或 `opc`,效果相同。

**更新三个 AI 工具(`--tool`):** MuxLM 会在 `PATH` 中找到这三个工具,识别各自来自 npm、Homebrew 还是官方安装程序,并交给对应的官方更新命令——不会改变你的安装方式。未安装的工具自动跳过;其中一个失败不影响其余。如果某工具版本太旧、还不支持安全的自动更新,MuxLM 会提示你先按原方式升级一次,而不会误启它的交互界面。

**Catalog 自动更新:** 每次正常启动都会顺带检查模型列表,合法更新会原子写入并可立即用于当前命令;检查失败时继续使用上次的有效缓存或内置版本兜底——所以**离线也能用**。发现新版程序只会提示,**绝不**静默替换二进制。

```bash
MUXLM_AUTO_UPDATE=0 cld glm      # 关闭启动检查
MUXLM_UPDATE_DEBUG=1 cld glm     # 打印更新诊断
cld update                        # 立即更新模型列表
```

通过本文命令安装的 MuxLM 可以自我更新。若当前副本来自其它地方,程序会停下并提示你沿用原来的安装方式,不会贸然覆盖文件。

## 隐私

- 子进程只拿到**当前 provider** 的 key,其它 provider key 会从环境中清理掉。
- Codex 和 OpenCode 使用**一次性配置目录**,不会动你的全局配置。
- API key 优先保存在 macOS Keychain 或 Linux Secret Service。

## 进阶

### 自建 Catalog 服务器

把 `catalog.json` 放在静态 HTTPS 地址(建议支持 `ETag` 或 `Last-Modified`),然后设置:

```bash
export MUXLM_CATALOG_URL=https://example.com/catalog.json
```

在你迁移到自己的服务器之前,默认 catalog 由本 GitHub 仓库提供。下载上限为 2 MiB,并经过严格 schema 校验、revision 单调不可变校验、回滚保护、tombstone 和信任字段校验。

### Catalog 更新的安全机制

Catalog 更新并非只有"新增":新 revision 可以增加 provider/model,也可以退役并删除旧模型或别名,以及移动 `latest`。为防止后续更新带来意外,这些规则始终生效:

- Catalog revision **单调递增且不可变**——同一 revision 不能被篡改,也不能回滚。
- 模型退役后留下**永久 tombstone**,已退役的版本别名不会被后来的更新重新启用。
- **官方模型短名**以及"来源 + 模型短名"的目标,不能被后续更新悄悄重定向到别的来源。
- Provider 的**信任字段**不能被静默修改。

### 旧版工具迁移

配置目录依次采用 `MUXLM_CONFIG_DIR` → `PROVIDERDECK_CONFIG_DIR` → `CX_CONFIG_DIR`;均未设置时,Linux 使用 `$XDG_CONFIG_HOME/muxlm` 或 `~/.config/muxlm`,macOS 默认使用 `~/.config/muxlm`。环境变量优先级为 `MUXLM_*` > `PROVIDERDECK_*` > `CX_*`。已有的 ProviderDeck 和 ez-switch(cx) 配置与密钥仍可读取,**不做破坏性迁移**;安装器会在确认安全时保留 `providerdeck`、`ez-switch` 兼容命令。

### 从源码构建

```bash
go test ./...
go build -ldflags "-X main.appVersion=v2.3.0" -o muxlm .
```

采用 [MIT License](LICENSE)。种子 catalog 含有社区来源数据,详见[第三方声明](THIRD_PARTY_NOTICES.md)。
