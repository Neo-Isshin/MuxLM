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

> 仓库在自建 Gitea（私有），克隆/下载需带 token。

```bash
# 一键脚本（需先发布 release 资产；私有仓库请在 URL 里带 token）
curl -fsSL https://gitea.nxc8335.cloud/nxc8335/ez-switch/raw/branch/main/install.sh | bash

# 或源码构建（需要 Go）
git clone https://gitea.nxc8335.cloud/nxc8335/ez-switch && cd ez-switch
go build -o ez-switch .
install -m755 ez-switch ~/.local/bin
( cd ~/.local/bin && ln -sf ez-switch cdx && ln -sf ez-switch cld && ln -sf ez-switch opc )
```

> 入口名 `cdx`/`cld`/`opc` 已避开常见系统命令（`cc` 编译器、`oc` OpenShift 等），装进 `~/.local/bin` 无遮盖风险。

## 用法

```bash
cld glm                           # claude + GLM 最新 (glm-5.2)
cdx glm                           # codex + GLM
opc m                             # opencode + MiniMax
cld m -y                          # claude + MiniMax + 跳过权限
cdx m --intl                      # codex + MiniMax 海外端点 (api.minimax.io)
opc ds -m deepseek-reasoner
cld glm -- "fix the bug"          # -- 之后透传给底层 CLI
```

### 命名规则

- **裸别名**（`glm` / `m` / `ds` …）永远解析到该厂商**最新**模型。厂商发新版时只需在 catalog 里挪动 `Latest` 标记，裸别名自动跟上。
- **版本别名**（`glm52` / `m3` …）= 裸别名 + 版本号，**锁定**具体版本。
- `-m <id>` 可在任意别名上再次覆盖模型。

## API Key

两种方式，任选：

1. **环境变量**（推荐）：`export GLM_KEY=...`（变量名见对照表的 `*_KEY`）。
2. **首次交互输入**：运行时若没找到 key，会**隐藏输入**让你粘贴，并询问是否保存到 `~/.config/cx/keys.env`（权限 600）下次免输。

环境变量优先级高于 keys.env。

### 国内 / 海外端点（MiniMax、火山方舟、SiliconFlow）

这几家国内与海外是**两套独立账号、两把不同的 key**，工具按区域分开存：
- 国内 → `MINIMAX_KEY` / `ARK_API_KEY` / `SILICONFLOW_KEY`
- 海外 → `MINIMAX_KEY_INTL` / `ARK_API_KEY_INTL` / `SILICONFLOW_KEY_INTL`

首次输入时，若两边都没 key，会先让你**选端点**（显示各自域名，如 `api.minimaxi.com` / `api.minimax.io`），再按区域隐藏输入并保存。之后：
- 只存了国内 key → 走国内；只存了海外 key → 自动走海外。
- 两边都有 → 默认国内，`--intl` 强制海外。

## 对照表

`cld list`（或 `cdx list` / `opc list`）打印（节选）：

| 别名 | 版本别名 | 厂商 | 默认模型 | claude | codex | opencode | intl |
|---|---|---|---|:-:|:-:|:-:|:-:|
| `glm` | `glm52`·`glm51`·`glm47` | 智谱 GLM | glm-5.2 | ✅ | ✅ | ✅ | — |
| `kimi` | `kimi26` | Moonshot Kimi | kimi-k2.6 | ✅ | ✅ | ✅ | — |
| `m` | `m3`·`m27` | MiniMax | MiniMax-M3 | ✅ | ✅ | ✅ | `--intl` |
| `doubao` | `doubao` | 火山方舟 Doubao | doubao-seed-2-0-code-preview-latest | ✅ | ✅ | ✅ | `--intl` |
| `nv` | `nvl` | Nvidia NIM | meta/llama-3.1-405b-instruct | — | ✅ | ✅ | — |
| `ds` | `dsc`·`dsr` | DeepSeek | deepseek-chat | ✅ | ✅ | ✅ | — |
| `sf` | `sfv3` | SiliconFlow 硅基流动 | deepseek-ai/DeepSeek-V3 | — | ✅ | ✅ | `--intl` |

- `claude`/`codex`/`opencode` 列的 `✅`/`—`：该别名能否喂给对应 CLI，**由端点协议决定，不做协议代理**——claude 只吃 anthropic 端点，codex 只吃 openai 端点，opencode 两种都吃。所以只有 openai 端点的厂商（Nvidia NIM、SiliconFlow）不能喂 claude。
- `intl` 列：标 `--intl` 的厂商区分**国内/海外**两套端点（两套账号、两把 key），可用 `--intl` 切海外；`—` 表示只有一套端点。
- 保存的**自定义别名**（见下）也会出现在这张表里，厂商名显示为 `自定义 · <域名>`。

## 自定义端点（custom）

内置 catalog 没有你要的厂商？直接临时接入，不用改代码：

```bash
cdx custom        # codex（openai 协议）
cld custom        # claude（anthropic 协议）
opc custom        # opencode（会让你选 openai / anthropic）
```

流程：依次输入 **端点 base URL / model id / API key（隐藏输入）** → 工具发一个最小请求做**可用性探测**，2xx 才放行：

- ✓ 可用（2xx）→ 继续；✗ key 无效/无权限（401/403）、端点路径不对（404）、触发限流（429）、连不上 都会**中止**，不启动。
- 探测通过后可选**保存为自定义别名**（存 `~/.config/cx/custom.json`，权限 600，明文 key），下次直接 `cdx <别名>` 用、无需重输；保存的别名会出现在 `cld list` 中，key 内联存储、不走环境变量。

> openai 协议会自动先试 `/chat/completions`，404 再试 `/v1/chat/completions`，所以 base URL 填到域名级即可（如 `https://api.deepseek.com`）。anthropic 协议固定走 `/v1/messages`。

## 实现要点

- **claude**（`cld`）：inline 设 `ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN` 后 exec，**不碰全局** `~/.claude/settings.json`。
- **codex**（`cdx`）：生成一次性 `CODEX_HOME`（临时 `config.toml` + `auth.json`），跑完即弃，不碰 `~/.codex`。
- **opencode**（`opc`）：生成一次性 `OPENCODE_CONFIG_DIR`（临时 `opencode.json`），不碰全局配置。
- `-y`：claude→`--dangerously-skip-permissions`，codex→`--dangerously-bypass-approvals-and-sandbox`，opencode→写入宽松权限配置 + `--force`（opencode 无单独的 bypass flag）。

## 增删 provider

- **临时接入某个厂商**：用 `custom`（见上），存进 `~/.config/cx/custom.json`，**无需改代码/重编译**。
- **增删内置厂商**（维护者）：编辑 `catalog.go` 的 `providers` 切片（端点参考 cc-switch 的 `src/config/*ProviderPresets.ts`）。带 `*Intl` 字段的会自动获得 `--intl` 海外端点。

## 注意

- 种子 catalog 的端点取自社区（cc-switch，MIT）并需按你的账号核对；模型 id 随厂商更新需维护（`Latest` 标记决定裸别名指向）。
- opencode / codex 的具体 flag 以你本地安装的版本为准（opencode 未安装时其权限配置语法待实测）。
