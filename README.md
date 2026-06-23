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

```bash
# 方式一：一键脚本（发布 release 后）
curl -fsSL https://raw.githubusercontent.com/OWNER/ez-switch/main/install.sh | bash

# 方式二：源码（需要 Go）
git clone https://github.com/OWNER/ez-switch && cd ez-switch
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

### 国内 / 海外端点（MiniMax、SiliconFlow）

这两家国内与海外是**两套独立账号、两把不同的 key**，工具按区域分开存：
- 国内 → `MINIMAX_KEY` / `SILICONFLOW_KEY`
- 海外 → `MINIMAX_KEY_INTL` / `SILICONFLOW_KEY_INTL`

首次输入时，若两边都没 key，会先让你**选端点**（显示各自域名，如 `api.minimaxi.com` / `api.minimax.io`），再按区域隐藏输入并保存。之后：
- 只存了国内 key → 走国内；只存了海外 key → 自动走海外。
- 两边都有 → 默认国内，`--intl` 强制海外。

## 对照表

`cld list`（或 `cdx list` / `opc list`）打印（节选）：

| 别名 | 版本别名 | 厂商 | 默认模型 | claude | codex | opencode | intl |
|---|---|---|---|:-:|:-:|:-:|:-:|
| `glm` | `glm52`·`glm51`·`glm47` | 智谱 GLM | glm-5.2 | ✅ | ✅ | ✅ | — |
| `kimi` | `kimi26` | Moonshot Kimi | kimi-k2.6 | ✅ | ✅ | ✅ | — |
| `m` | `m3`·`m27` | MiniMax | MiniMax-M3 | ✅ | ✅ | ✅ | ✅ |
| `opus` | `opus46` | Claude Opus(中转) | claude-opus-4-6 | ✅ | — | ✅ | — |
| `sonnet` | `sonnet46` | Claude Sonnet(中转) | claude-sonnet-4-6 | ✅ | — | ✅ | — |
| `haiku` | `haiku45` | Claude Haiku(中转) | claude-haiku-4-5-20250514 | ✅ | — | ✅ | — |
| `ds` | `dsc`·`dsr` | DeepSeek | deepseek-chat | ✅ | ✅ | ✅ | — |
| `sf` | `sfv3` | SiliconFlow 硅基流动 | deepseek-ai/DeepSeek-V3 | — | ✅ | ✅ | ✅ |

`✅`/`—` 表示该别名能在哪个 CLI 用——**由厂商端点协议决定，不做协议代理**：
- claude 只吃 anthropic 端点；codex 只吃 openai 端点；opencode 两种都吃。
- 所以 Claude 系（opus/sonnet/haiku，只有 anthropic 端点）不能喂 codex；OpenAI 系不能喂 claude。
- 对于 ChatGPT / 原生 Opus 等，用户直接用厂商自家的 harness CLI 即可，本工具不涉及。

## 实现要点

- **claude**（`cld`）：inline 设 `ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN` 后 exec，**不碰全局** `~/.claude/settings.json`。
- **codex**（`cdx`）：生成一次性 `CODEX_HOME`（临时 `config.toml` + `auth.json`），跑完即弃，不碰 `~/.codex`。
- **opencode**（`opc`）：生成一次性 `OPENCODE_CONFIG_DIR`（临时 `opencode.json`），不碰全局配置。
- `-y`：claude→`--dangerously-skip-permissions`，codex→`--dangerously-bypass-approvals-and-sandbox`，opencode→写入宽松权限配置 + `--force`（opencode 无单独的 bypass flag）。

## 增删 provider

编辑 `catalog.go` 的 `providers` 切片即可（端点参考 cc-switch 的 `src/config/*ProviderPresets.ts`）。带 `*Intl` 字段的会自动获得 `--intl` 海外端点。

## 注意

- 种子 catalog 的端点取自社区（cc-switch，MIT）并需按你的账号核对；模型 id 随厂商更新需维护（`Latest` 标记决定裸别名指向）。
- opencode / codex 的具体 flag 以你本地安装的版本为准（opencode 未安装时其权限配置语法待实测）。
