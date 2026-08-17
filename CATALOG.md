# MuxLM Catalog

[简体中文](CATALOG.zh-CN.md) · [Raw catalog data](catalog-v2.json)

This reference follows the layout of `cld list` and separates first-party model routes from relay routes. It reflects the embedded catalog revision `2026-08-17.2`; because MuxLM can update its catalog independently, run `cld list` to see the catalog currently active on your machine.

Use `cdx`, `cld`, or `opc` as the entry command. A model short name by itself selects its official route; prefix it with a source alias to select a relay, for example `cld k3` versus `opc or k3`.

## Official routes

```text
Alias (versions)             Provider                              Default model                     Entry        intl
----------------------------------------------------------------------------------------------------------------------
def                          Native account / configuration         Determined by the selected CLI    cld/cdx/opc  —
glm (glm53,glm52,glm51)      Zhipu GLM (pay-as-you-go API)          glm-5.3                           cld/cdx/opc  —
    (glm5v,glm5,glm47)
    (glm47fx,glm47f)
glmc (glmc53,glmc52,glmc5v)  Zhipu GLM Coding Plan                  glm-5.3                           cld/cdx/opc  —
     (glmc51,glmc5t,glmc47)
k (k27,k27h,k26)             Moonshot Kimi (pay-as-you-go API)      kimi-k3                           cld/cdx/opc  —
    Optional short name: k3
kc                           Kimi for Coding                        kimi-for-coding                   cld/cdx/opc  —
m (m27std,m27,m25std)        MiniMax                                MiniMax-M3                        cld/cdx/opc  --intl
  (m25,m21,m2)
    Optional short name: m3
doubao                       Volcengine Coding Plan                 ark-code-latest                   cld/cdx/opc  —
nv (nvn35l)                  NVIDIA NIM                             nvidia/nemotron-3.5-lightning-    cdx/opc      —
                                                                  30b-a3b
    Optional short name: n35l
ds (dsv4f,dsv4p)             DeepSeek                               deepseek-v4-pro                   cld/cdx/opc  —
q (q37,q37m,q36,q36f)        Alibaba Cloud Bailian API              qwen3.7-plus                      cld/cdx/opc  —
  (q35f,qcn,qcp)
qc (qc37,qc36,qc35)          Alibaba Cloud Bailian Coding Plan      qwen3.7-plus                      cld/cdx/opc  —
   (qc3m,qccn,qccp)
```

## Relay routes

```text
Alias (versions)             Provider                              Default model                     Entry        intl
----------------------------------------------------------------------------------------------------------------------
nv (nvgpt)                   NVIDIA NIM (third-party model)         — fixed alias only                cdx/opc      —
sf (sfv4f,sfv4p,sfglm52)     SiliconFlow                           deepseek-ai/DeepSeek-V4-Flash     cld/cdx/opc  --intl
   (sfk26)
    Optional: sf dsv4f, sf dsv4p, sf k27, sf glm52, sf k26
qc (qck25,qcglm5,qcm25)      Bailian Coding Plan (third-party)      — fixed aliases only              cld/cdx/opc  —
   (qcglm47)
or (ors5,oro5,oro5f)         OpenRouter                             anthropic/claude-sonnet-5         cdx/opc      —
   (oro48,ors46,org56)
   (ordsv4p,ordsv4f,orq38m)
   (orq3827,orq3824t,orgem37f)
   (orn35l,orq37f,orqcn)
   (orglm52,ork3,orm3)
    Optional: or s5, or o5, or o5f, or o48, or s46, or g56,
              or dsv4p, or dsv4f, or q38m, or q3827, or q3824t,
              or gem37f, or n35l, or q37f, or qcn, or glm52, or k3, or m3
```

The alias before parentheses always selects that provider's latest model. Names in parentheses are pinned version aliases. Items shown as “Optional” use the two-part form `<source> <model>`.
