package main

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

// providers 是内置种子 catalog（可由维护者增删；终端用户用别名直接切）。
// 端点取自 cc-switch 社区 catalog（MIT）并按需核对；带 Intl 的为国内/海外双端点。
var providers = []Provider{
	{
		Alias:     "glm",
		Name:      "智谱 GLM",
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
		Alias:     "kimi",
		Name:      "Moonshot Kimi",
		ClaudeURL: "https://api.moonshot.cn/anthropic",
		OpenAIURL: "https://api.moonshot.cn/v1",
		KeyEnv:    "KIMI_KEY",
		CLI:       []string{"claude", "codex", "opencode"},
		Models: []Model{
			{ID: "kimi-k2.6", Tag: "kimi26", Latest: true},
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
		Alias:     "opus",
		Name:      "Claude Opus (MiniMax 中转)",
		ClaudeURL: "https://api.minimaxi.com/anthropic",
		KeyEnv:    "MINIMAX_KEY",
		CLI:       []string{"claude", "opencode"},
		Models:    []Model{{ID: "claude-opus-4-6", Tag: "opus46", Latest: true}},
	},
	{
		Alias:     "sonnet",
		Name:      "Claude Sonnet (MiniMax 中转)",
		ClaudeURL: "https://api.minimaxi.com/anthropic",
		KeyEnv:    "MINIMAX_KEY",
		CLI:       []string{"claude", "opencode"},
		Models:    []Model{{ID: "claude-sonnet-4-6", Tag: "sonnet46", Latest: true}},
	},
	{
		Alias:     "haiku",
		Name:      "Claude Haiku (MiniMax 中转)",
		ClaudeURL: "https://api.minimaxi.com/anthropic",
		KeyEnv:    "MINIMAX_KEY",
		CLI:       []string{"claude", "opencode"},
		Models:    []Model{{ID: "claude-haiku-4-5-20250514", Tag: "haiku45", Latest: true}},
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
// 裸别名 → Latest 模型；Tag → 该版本模型。
func buildIndex() map[string]Resolved {
	idx := make(map[string]Resolved)
	for i := range providers {
		p := &providers[i]
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
	return idx
}
