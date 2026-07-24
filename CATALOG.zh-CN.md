# MuxLM Catalog 参照表

[English](CATALOG.md) · [Catalog 原始数据](catalog.json)

本表沿用 `cld list` 的展示方式，并按模型的实际来源分为官方来源和中转来源。内容对应内置 Catalog revision `2026-07-23.4`；由于 MuxLM 可以独立更新 Catalog，请以本机执行 `cld list` 后显示的内容为准。

入口可以使用 `cdx`、`cld` 或 `opc`。单独使用模型短名时选择官方来源；需要中转来源时，在模型短名前增加来源短名，例如 `cld k3` 与 `opc or k3`。

## 官方来源

```text
别名（版本）                 Provider                              默认模型                           入口         intl
----------------------------------------------------------------------------------------------------------------------
def                          原生账号 / 配置                       由对应 CLI 决定                   cld/cdx/opc  —
glm (glm52,glm51,glm47)      智谱 GLM（按量计费 API）              glm-5.2                            cld/cdx/opc  —
glmc                         智谱 GLM Coding Plan（订阅）          glm-5.2                            cld/cdx/opc  —
k (k27,k26)                  Moonshot Kimi（按量计费 API）         kimi-k3                            cld/cdx/opc  —
    可选短名: k3
kc                           Kimi for Coding（订阅）               kimi-for-coding                    cld/cdx/opc  —
m (m27std,m27)               MiniMax                               MiniMax-M3                         cld/cdx/opc  --intl
    可选短名: m3
doubao                       火山方舟 Coding Plan（订阅）          ark-code-latest                    cld/cdx/opc  —
ds (dsv4f,dsv4p)             DeepSeek                              deepseek-v4-pro                    cld/cdx/opc  —
q (q37,q37m,qcn,qcp)         阿里云百炼（按量计费 API）            qwen3.7-plus                       cld/cdx/opc  —
qc (qc37,qc36,qc35)          阿里云百炼 Coding Plan（订阅）        qwen3.7-plus                       cld/cdx/opc  —
   (qc3m,qccn,qccp)
```

## 中转来源

```text
别名（版本）                 Provider                              默认模型                           入口         intl
----------------------------------------------------------------------------------------------------------------------
nv (nvgpt)                   Nvidia NIM                             openai/gpt-oss-120b                cdx/opc      —
sf (sfv4f,sfv4p)             SiliconFlow 硅基流动                  deepseek-ai/DeepSeek-V4-Flash      cld/cdx/opc  --intl
    可选: sf dsv4f, sf dsv4p, sf k27
qc (qck25,qcglm5,qcm25)      百炼 Coding Plan（第三方模型）         —（仅固定版本别名）                cld/cdx/opc  —
   (qcglm47)
or (ors5,oro48,ors46)        OpenRouter                            anthropic/claude-sonnet-5          cdx/opc      —
   (org56,orqcn,orglm52)
   (ork3,orm3)
    可选: or s5, or o48, or s46, or g56, or qcn, or glm52, or k3, or m3
```

括号前的来源别名始终选择该 provider 的最新模型；括号内是固定版本别名。“可选”行使用 `<来源短名> <模型短名>` 两段式命令。
