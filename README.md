# ez-switch — 快速切换模型

一个极简的命令行小工具，**唯一功能**：一行命令在 `codex` / `claude` / `opencode` 之间快速切换模型/provider。

> 灵感来自 [cc-switch](https://github.com/farion1231/cc-switch)（MIT）的 provider catalog，但只保留「快速切换」这一件事——没有 GUI、没有 TUI、没有数据库、没有协议代理。第三方厂商基本都同时提供 anthropic / openai 兼容端点，直连即可。

## 一个工具，三个入口

| 命令 | 等价于 |
|---|---|
| `cdx <别名>` | `codex`  + `<别名>` |
| `cld <别名>` | `claude` + `<别名>` |
| `opc <别名>` | `opencode` + `<别名>` |

`cdx` / `cld` / `opc` 是同一个二进制（`ez-switch`）的软链，程序靠自己被叫的名字（argv[0]）决定走哪个 CLI。

## 安装

> 仓库在自建 Gitea（私有），需先在仓库里发一份对应平台（`ez-switch-darwin-arm64` / `ez-switch-darwin-amd64` / `ez-switch-linux-arm64` / `ez-switch-linux-amd64`）的 release 资产。

```bash
curl -fsSL https://gitea.nxc8335.cloud/nxc8335/ez-switch/raw/branch/main/install.sh | bash
```

安装器会同时下载 release 的 `SHA256SUMS`，校验通过后才原子替换本地二进制；下载中断或校验失败不会覆盖旧版本。

入口名 `cdx` / `cld` / `opc` 是同一个二进制 `ez-switch` 的三个软链（程序靠 `argv[0]` 决定走 `codex` / `claude` / `opencode`），装进 `~/.local/bin` 无遮盖风险。

## 用法

```bash
cld glm                           # claude + GLM 最新 (glm-5.2)
cdx glm                           # codex + GLM
opc m                             # opencode + MiniMax
cld m -y                          # claude + MiniMax + 跳过权限
cdx m --intl                      # codex + MiniMax 海外端点 (api.minimax.io)
opc ds -m deepseek-reasoner
cld glm -- "fix the bug"          # -- 之后透传给底层 CLI

cld config                        # 列出 Claude 可用 provider 及 key 状态
cld add                           # 选择 provider/套餐并增加具名 key
cld set-key m                     # 给 MiniMax 再增加一把 key
cld remove m                      # 删除 MiniMax 的本地配置（需确认）
cld update                        # 只更新 catalog，不更新二进制
```

### 命名规则

- **裸别名**（`glm` / `m` / `ds` …）永远解析到该厂商**最新**模型。厂商发新版时只需在 catalog 里挪动 `Latest` 标记，裸别名自动跟上。
- **版本别名**（`glm52` / `m3` …）= 裸别名 + 版本号，**锁定**具体版本。
- `-m <id>` 可在任意别名上再次覆盖模型。

## Provider 与 API Key 管理

provider 是一级配置单位，模型别名只是它的路由。因此 `m` / `m3` / `m27` 都属于 `minimax`，共用同一组 key，不会因为切换模型而重新要求输入。

一个 provider 可以保存多把**具名 key**：

```bash
cld add          # 选择 MiniMax，名称可用 payg / coding / key1 ...
cld set-key m    # 再增加一把
cld m3           # 有多把时列出名称，回车默认选第一把
```

在多 key 选择界面输入 `x`，再选编号并输入 `yes` 确认，可删除某把已保存 key。`cld config` 只显示 key 名称、数量和存储来源，永不显示 key 内容。

GLM / Kimi 的按量计费与 Coding Plan 使用同一 provider 目录，但用 `plan=standard/coding` 隔离：`glm` 选按量 key，`glmc` 选 Coding Plan key。MiniMax 两类 key 使用相同端点/模型，可以直接命名为 `payg` 和 `coding`，启动时选择。

仍支持 `GLM_KEY` / `MINIMAX_KEY` 等环境变量和旧 `~/.config/cx/keys.env`；环境变量优先显示为可用 key，但不会被复制到本地存储。

### 密钥存储与隐私

- macOS 默认使用系统 Keychain；Linux 有 `secret-tool` 时使用 Secret Service。
- 系统 secret service 不可用时，回退到 provider 目录下的 `secrets.json`（明文、600）并显示警告。可用 `CX_SECRET_BACKEND=file` 显式选择该后端。
- provider 元数据和 key 元数据位于 `~/.config/cx/providers/<provider>/`，只保存 `secret_ref`，不内联 key。
- 配置目录会收紧到 700，文件收紧到 600；写入采用同目录原子 rename，并拒绝密钥文件/目录符号链接。
- key 不接受命令行 `--key` 参数，避免进入 shell history 和进程列表。底层 CLI 也不会继承其他 provider 的 key 环境变量。

### 设置期可用性检测（仅首次）

无论内置厂商还是自定义端点，**首次**输入 key 时工具会向端点发一个最小请求做可用性检测——避免把一个拼写错的 key 存下来：

- **内置厂商**（`*_KEY`）：端点明确返回 401/403 视为 key 错，提示**重新输入**；其它情况（含暂时连不上）都接受并保存。
- **自定义端点**（`custom`）：必须 2xx 才放行；失败则**重新输入**端点信息。
- key 已保存或来自环境变量时**直接启动、不再检测**——检测只在「设置」时发生一次。

### 国内 / 海外端点（MiniMax、火山方舟、SiliconFlow）

这几家国内与海外是**两套独立账号、两把不同的 key**，工具按区域分开存：
- 国内 → `MINIMAX_KEY` / `ARK_API_KEY` / `SILICONFLOW_KEY`
- 海外 → `MINIMAX_KEY_INTL` / `ARK_API_KEY_INTL` / `SILICONFLOW_KEY_INTL`

首次输入时，若两边都没 key，会先让你**选端点**（显示各自域名，如 `api.minimaxi.com` / `api.minimax.io`），再按区域隐藏输入并保存。之后：
- 只存了国内 key → 走国内；只存了海外 key → 自动走海外。
- 两边都有 → 默认国内，`--intl` 强制海外。

### Coding Plan（订阅）端点

Kimi 和 GLM 都有独立的「Coding 订阅套餐」，其**端点 / 模型 / key 与普通按量计费 API 不同**，故拆成单独别名：

| 别名 | 用途 | 端点（与普通 API 的差异） | key |
|---|---|---|---|
| `kimic` | Kimi for Coding 订阅 | anthropic `api.kimi.com/coding`（与普通 `api.moonshot.cn/v1` **不同域名**）；**模型必须用 `kimi-for-coding`**，发 `kimi-k2.x` 会被拒 | `KIMI_CODING_KEY` |
| `glmc` | GLM Coding Plan 订阅 | anthropic 端点与普通相同（`/api/anthropic`，靠 key 区分套餐）；openai 协议须用专属 `open.bigmodel.cn/api/coding/paas/v4`；模型 id 不变 | `GLM_CODING_KEY` |

普通按量计费走 `kimi` / `glm`（`KIMI_KEY` / `GLM_KEY`）。Kimi 的 Coding 端点只接受 `kimi-for-coding`，所以普通 Kimi API（`kimi`）只有 openai 端点、不支持 claude——要用 claude 就走订阅 `kimic`。

> 其余厂商无此拆分：MiniMax Coding 订阅与按量计费用**同一个** `/anthropic` 端点、同一模型 `MiniMax-M3`，**仅 key 不同**（订阅 key 为 `sk-cp-` 前缀），直接把订阅 key 填进 `MINIMAX_KEY` 即可，无需单独别名；DeepSeek / 火山方舟 / SiliconFlow / Nvidia NIM 均只有一套标准 API。

## 对照表

`cld list`（或 `cdx list` / `opc list`）打印（节选）：

| 别名 | 版本别名 | 厂商 | 默认模型 | claude | codex | opencode | intl |
|---|---|---|---|:-:|:-:|:-:|:-:|
| `glm` | `glm52`·`glm51`·`glm47` | 智谱 GLM（按量计费） | glm-5.2 | ✅ | ✅ | ✅ | — |
| `glmc` | — | 智谱 GLM Coding Plan | glm-5.2 | ✅ | ✅ | ✅ | — |
| `kimi` | `kimi26` | Moonshot Kimi（按量计费） | kimi-k2.6 | — | ✅ | ✅ | — |
| `kimic` | — | Kimi for Coding | kimi-for-coding | ✅ | — | ✅ | — |
| `m` | `m3`·`m27` | MiniMax | MiniMax-M3 | ✅ | ✅ | ✅ | `--intl` |
| `doubao` | `doubao-code` | 火山方舟 Doubao | doubao-seed-code-preview-latest | ✅ | ✅ | ✅ | `--intl` |
| `nv` | `nvgpt` | Nvidia NIM | openai/gpt-oss-120b | — | ✅ | ✅ | — |
| `ds` | `dsv4f`·`dsv4p` | DeepSeek | deepseek-v4-flash | ✅ | ✅ | ✅ | — |
| `sf` | `sfv4f`·`sfv4p` | SiliconFlow 硅基流动 | deepseek-ai/DeepSeek-V4-Flash | — | ✅ | ✅ | `--intl` |

- `claude`/`codex`/`opencode` 列的 `✅`/`—`：该别名能否喂给对应 CLI，**由端点协议决定，不做协议代理**——claude 只吃 anthropic 端点，codex 只吃 openai 端点，opencode 两种都吃。所以只有 openai 端点的厂商（Nvidia NIM、SiliconFlow）不能喂 claude。
- `intl` 列：标 `--intl` 的厂商区分**国内/海外**两套端点（两套账号、两把 key），可用 `--intl` 切海外；`—` 表示只有一套端点。
- 保存的**自定义别名**（见下）也会出现在这张表里，厂商名显示为 `自定义 · <域名>`。

## 自定义端点

内置 catalog 没有你要的厂商？直接临时接入，不用改代码：

```bash
cdx add            # 选 custom，codex 使用 openai 协议
cld add            # 选 custom，claude 使用 anthropic 协议
opc add            # 选 custom，再选 openai / anthropic
```

流程：依次输入 **端点 base URL / model id / API key（隐藏输入）** → 工具发一个最小请求做**可用性探测**，2xx 才放行：

- ✓ 可用（2xx）→ 继续；✗ 不可用（401/403/404/429/连不上）则**提示重新输入**端点信息，不启动。
- 公网端点默认必须是 HTTPS；localhost 可用 HTTP。其它 HTTP 端点必须显式输入 `insecure` 才能继续。
- 探测通过后写入 `~/.config/cx/providers/custom-<alias>/provider.json`，key 走统一 secret backend，不内联到 provider JSON。

> openai 协议会自动先试 `/chat/completions`，404 再试 `/v1/chat/completions`，所以 base URL 填到域名级即可（如 `https://api.deepseek.com`）。anthropic 协议固定走 `/v1/messages`。

## 实现要点

- **claude**（`cld`）：inline 设 `ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN` 后 exec，**不碰全局** `~/.claude/settings.json`。
- **codex**（`cdx`）：生成一次性 `CODEX_HOME`（临时 `config.toml` + `auth.json`），成功、失败退出后都会删除，不碰 `~/.codex`。
- **opencode**（`opc`）：生成一次性 `OPENCODE_CONFIG_DIR`（临时 `opencode.json`），成功、失败后都会删除。
- `-y`：claude→`--dangerously-skip-permissions`，codex→`--dangerously-bypass-approvals-and-sandbox`，opencode→写入宽松权限配置 + `--auto`。

## Catalog 更新

`cld update`（三个入口均可）从仓库的 HTTPS raw URL 下载 `catalog.json`，执行 2 MiB 限制、严格 JSON schema/字段校验、别名唯一性检查、HTTPS 端点检查，再原子写入 `~/.config/cx/catalog.json`。更新失败或本地 catalog 损坏时自动回退到二进制内置 catalog。

catalog 不允许包含 key，也不包含可执行代码。命令会输出 revision、provider 数量和下载内容的 SHA-256 摘要。当前信任边界是配置的 HTTPS 仓库；SHA-256 用于记录/对比，不等于独立数字签名。

## 注意

- 源码构建使用 `go.mod` 指定的 Go 1.26.5+ 工具链（Go 会按需自动下载），避免用含已知 TLS 漏洞的旧标准库产出 release。
- 种子 catalog 的端点取自社区（cc-switch，MIT）并需按你的账号核对；模型 id 随厂商更新需维护（`Latest` 标记决定裸别名指向）。
- opencode / codex / claude 的 flag 可能随上游变化；release 前应继续用当前官方 CLI/文档复核。
