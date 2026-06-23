package main

import "strings"

// Model 是某个 provider 下的一个具体模型。
type Model struct {
	ID     string // 真实模型 id，如 "glm-5.2"，会带版本号
	Tag    string // 版本别名，如 "glm52"；为空表示无独立别名
	Latest bool   // 是否当前最新（裸别名 alias 解析到这条）
}

// Provider 是一个可切换的供应商。
// 同一厂商区分 国内(cn,默认) / 海外(intl) 两套端点。
type Provider struct {
	Alias         string   // 裸别名，永远指向 Latest 模型，如 "glm"
	Name          string   // 展示名
	ClaudeURL     string   // anthropic 端点（claude 用；opencode 在无 openai 端点时回退到此）
	OpenAIURL     string   // openai 端点（codex 用；opencode 优先用此）
	ClaudeURLIntl string   // 海外 anthropic 端点（可选）
	OpenAIURLIntl string   // 海外 openai 端点（可选）
	KeyEnv        string   // 读取 key 的环境变量名
	Key           string   // 内联 key（custom 自定义别名用；非空时 getKey 直接返回，不走 env）
	CLI           []string // 支持的 CLI：claude / codex / opencode 的子集（由端点协议决定，无代理）
	WireAPI       string   // codex 的 wire_api：chat(默认) / responses
	Models        []Model
}

func (p *Provider) supports(cli string) bool {
	for _, c := range p.CLI {
		if c == cli {
			return true
		}
	}
	return false
}

func (p *Provider) hasIntl() bool {
	return p.ClaudeURLIntl != "" || p.OpenAIURLIntl != ""
}

func (p *Provider) claudeURL(intl bool) string {
	if intl && p.ClaudeURLIntl != "" {
		return p.ClaudeURLIntl
	}
	return p.ClaudeURL
}

func (p *Provider) openaiURL(intl bool) string {
	if intl && p.OpenAIURLIntl != "" {
		return p.OpenAIURLIntl
	}
	return p.OpenAIURL
}

func (p *Provider) wireAPI() string {
	if p.WireAPI == "" {
		return "chat"
	}
	return p.WireAPI
}

// probeTarget 按目标 CLI 选出本次启动实际要用的 协议 + base_url，供设置期可用性探测。
// claude→anthropic 端点；codex→openai 端点；opencode 优先 openai，没有则回退 anthropic。
func (p *Provider) probeTarget(cli string, intl bool) (protocol, base string) {
	switch cli {
	case "claude":
		return "anthropic", p.claudeURL(intl)
	case "codex":
		return "openai", p.openaiURL(intl)
	default:
		if u := p.openaiURL(intl); u != "" {
			return "openai", u
		}
		return "anthropic", p.claudeURL(intl)
	}
}

// keyEnv 返回给定区域的 key 环境变量名。海外用 <KeyEnv>_INTL，国内用 KeyEnv。
// （国内/海外是两套独立账号、不同 key，故按区域区分存储。）
func (p *Provider) keyEnv(intl bool) string {
	if intl && p.hasIntl() {
		return p.KeyEnv + "_INTL"
	}
	return p.KeyEnv
}

// host 返回该区域的代表域名（交互提示时让用户认出是哪个端点）。
func (p *Provider) host(intl bool) string {
	u := p.ClaudeURL
	if intl {
		u = p.ClaudeURLIntl
	}
	if u == "" {
		u = p.OpenAIURL
		if intl {
			u = p.OpenAIURLIntl
		}
	}
	return hostOf(u)
}

// hostOf 从一个 base_url 里抠出域名。
func hostOf(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		return u[:i]
	}
	return u
}

// providers 是内置种子 catalog（可由维护者增删；终端用户用别名直接切）。
// 端点取自 cc-switch 社区 catalog（MIT）并按需核对；带 Intl 的为国内/海外双端点。
var providers = []Provider{
	{
		Alias:     "glm",
		Name:      "智谱 GLM（按量计费 API）",
		ClaudeURL: "https://open.bigmodel.cn/api/anthropic",
		OpenAIURL: "https://open.bigmodel.cn/api/paas/v4",
		KeyEnv:    "GLM_KEY",
		CLI:       []string{"claude", "codex", "opencode"},
		Models: []Model{
			{ID: "glm-5.2", Tag: "glm52", Latest: true},
			{ID: "glm-5.1", Tag: "glm51"},
			{ID: "glm-4.7", Tag: "glm47"},
		},
	},
	{
		// GLM Coding Plan（订阅）：anthropic 端点与按量计费相同（用 key 区分套餐）；
		// openai 协议必须用 Coding 专属端点 /api/coding/paas/v4。模型 id 不变。
		Alias:     "glmc",
		Name:      "智谱 GLM Coding Plan（订阅）",
		ClaudeURL: "https://open.bigmodel.cn/api/anthropic",
		OpenAIURL: "https://open.bigmodel.cn/api/coding/paas/v4",
		KeyEnv:    "GLM_CODING_KEY",
		CLI:       []string{"claude", "codex", "opencode"},
		Models: []Model{
			{ID: "glm-5.2", Tag: "", Latest: true},
		},
	},
	{
		// 普通 Kimi API 为 openai 协议（/v1）。注意：/anthropic 与 /coding 都是 Coding 订阅端点，
		// 且只接受 kimi-for-coding，发 kimi-k2.x 会被拒——见下面的 kimic。
		Alias:     "kimi",
		Name:      "Moonshot Kimi（按量计费 API）",
		OpenAIURL: "https://api.moonshot.cn/v1",
		KeyEnv:    "KIMI_KEY",
		CLI:       []string{"codex", "opencode"},
		Models: []Model{
			{ID: "kimi-k2.6", Tag: "kimi26", Latest: true},
		},
	},
	{
		// Kimi for Coding（订阅）：anthropic 端点 api.kimi.com/coding（注意与普通 api.moonshot.cn 不同域），
		// 模型必须是 kimi-for-coding（kimi-k2.x 会被拒）。实测 api.moonshot.cn/coding 返回 404，已弃用。
		Alias:     "kimic",
		Name:      "Kimi for Coding（订阅）",
		ClaudeURL: "https://api.kimi.com/coding",
		KeyEnv:    "KIMI_CODING_KEY",
		CLI:       []string{"claude", "opencode"},
		Models: []Model{
			{ID: "kimi-for-coding", Tag: "", Latest: true},
		},
	},
	{
		Alias:         "m",
		Name:          "MiniMax",
		ClaudeURL:     "https://api.minimaxi.com/anthropic",
		OpenAIURL:     "https://api.minimaxi.com/v1",
		ClaudeURLIntl: "https://api.minimax.io/anthropic",
		OpenAIURLIntl: "https://api.minimax.io/v1",
		KeyEnv:        "MINIMAX_KEY",
		CLI:           []string{"claude", "codex", "opencode"},
		Models: []Model{
			{ID: "MiniMax-M3", Tag: "m3", Latest: true},
			{ID: "MiniMax-M2.7-highspeed", Tag: "m27"},
		},
	},
	{
		Alias:         "doubao",
		Name:          "火山方舟 Doubao",
		ClaudeURL:     "https://ark.cn-beijing.volces.com/api/compatible",
		OpenAIURL:     "https://ark.cn-beijing.volces.com/api/v3",
		ClaudeURLIntl: "https://ark.ap-southeast.bytepluses.com/api/compatible",
		OpenAIURLIntl: "https://ark.ap-southeast.bytepluses.com/api/v3",
		KeyEnv:        "ARK_API_KEY",
		CLI:           []string{"claude", "codex", "opencode"},
		Models: []Model{
			{ID: "doubao-seed-2-0-code-preview-latest", Tag: "doubao", Latest: true},
		},
	},
	{
		Alias:     "nv",
		Name:      "Nvidia NIM",
		OpenAIURL: "https://integrate.api.nvidia.com/v1",
		KeyEnv:    "NVIDIA_API_KEY",
		CLI:       []string{"codex", "opencode"},
		Models: []Model{
			{ID: "meta/llama-3.1-405b-instruct", Tag: "nvl", Latest: true},
		},
	},
	{
		Alias:     "ds",
		Name:      "DeepSeek",
		ClaudeURL: "https://api.deepseek.com/anthropic",
		OpenAIURL: "https://api.deepseek.com",
		KeyEnv:    "DEEPSEEK_API_KEY",
		CLI:       []string{"claude", "codex", "opencode"},
		Models: []Model{
			{ID: "deepseek-chat", Tag: "dsc", Latest: true},
			{ID: "deepseek-reasoner", Tag: "dsr"},
		},
	},
	{
		Alias:         "sf",
		Name:          "SiliconFlow 硅基流动",
		OpenAIURL:     "https://api.siliconflow.cn",
		OpenAIURLIntl: "https://api.siliconflow.com",
		KeyEnv:        "SILICONFLOW_KEY",
		CLI:           []string{"codex", "opencode"},
		Models:        []Model{{ID: "deepseek-ai/DeepSeek-V3", Tag: "sfv3", Latest: true}},
	},
}

// Resolved 是别名解析结果。
type Resolved struct {
	Prov  *Provider
	Model *Model
}

// buildIndex 把所有别名（裸别名 + 每个 model 的 Tag）建索引。
// 裸别名 → Latest 模型；Tag → 该版本模型。内置 providers + 用户自定义 custom 别名。
func buildIndex() map[string]Resolved {
	idx := make(map[string]Resolved)
	add := func(ps []Provider) {
		for i := range ps {
			p := &ps[i]
			var latest *Model
			for j := range p.Models {
				m := &p.Models[j]
				if m.Tag != "" {
					idx[m.Tag] = Resolved{p, m}
				}
				if m.Latest {
					latest = m
				}
			}
			if latest != nil {
				idx[p.Alias] = Resolved{p, latest}
			}
		}
	}
	add(providers)
	add(loadCustomProfiles())
	return idx
}
