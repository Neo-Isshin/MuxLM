# MuxLM Catalog

[简体中文](CATALOG.zh-CN.md) · [Raw catalog data](catalog.json)

This reference follows the layout of `cld list` and separates first-party model routes from relay routes. It reflects the embedded catalog revision `2026-07-23.4`; because MuxLM can update its catalog independently, run `cld list` to see the catalog currently active on your machine.

Use `cdx`, `cld`, or `opc` as the entry command. A model short name by itself selects its official route; prefix it with a source alias to select a relay, for example `cld k3` versus `opc or k3`.

## Official routes

```text
Alias (versions)             Provider                              Default model                     Entry        intl
----------------------------------------------------------------------------------------------------------------------
def                          Native account / configuration         Determined by the selected CLI    cld/cdx/opc  —
glm (glm52,glm51,glm47)      Zhipu GLM (pay-as-you-go API)          glm-5.2                           cld/cdx/opc  —
glmc                         Zhipu GLM Coding Plan                  glm-5.2                           cld/cdx/opc  —
k (k27,k26)                  Moonshot Kimi (pay-as-you-go API)      kimi-k3                           cld/cdx/opc  —
    Optional short name: k3
kc                           Kimi for Coding                        kimi-for-coding                   cld/cdx/opc  —
m (m27std,m27)               MiniMax                                MiniMax-M3                        cld/cdx/opc  --intl
    Optional short name: m3
doubao                       Volcengine Coding Plan                 ark-code-latest                   cld/cdx/opc  —
ds (dsv4f,dsv4p)             DeepSeek                               deepseek-v4-pro                   cld/cdx/opc  —
q (q37,q37m,qcn,qcp)         Alibaba Cloud Bailian API              qwen3.7-plus                      cld/cdx/opc  —
qc (qc37,qc36,qc35)          Alibaba Cloud Bailian Coding Plan      qwen3.7-plus                      cld/cdx/opc  —
   (qc3m,qccn,qccp)
```

## Relay routes

```text
Alias (versions)             Provider                              Default model                     Entry        intl
----------------------------------------------------------------------------------------------------------------------
nv (nvgpt)                   NVIDIA NIM                             openai/gpt-oss-120b               cdx/opc      —
sf (sfv4f,sfv4p)             SiliconFlow                           deepseek-ai/DeepSeek-V4-Flash     cld/cdx/opc  --intl
    Optional: sf dsv4f, sf dsv4p, sf k27
qc (qck25,qcglm5,qcm25)      Bailian Coding Plan (third-party)      — fixed aliases only              cld/cdx/opc  —
   (qcglm47)
or (ors5,oro48,ors46)        OpenRouter                             anthropic/claude-sonnet-5         cdx/opc      —
   (org56,orqcn,orglm52)
   (ork3,orm3)
    Optional: or s5, or o48, or s46, or g56, or qcn, or glm52, or k3, or m3
```

The alias before parentheses always selects that provider's latest model. Names in parentheses are pinned version aliases. Items shown as “Optional” use the two-part form `<source> <model>`.
