package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CustomProfile 是用户自定义保存的端点（存于 ~/.config/cx/custom.json，权限 600）。
type CustomProfile struct {
	Protocol string // "anthropic" | "openai"
	Base     string // 端点 base URL
	Model    string // 模型 id
	Key      string // API key（明文存储，文件权限 600）
}

func customFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cx", "custom.json")
}

// loadCustomProfiles 读 custom.json，转成可挂进索引的 Provider 列表。
func loadCustomProfiles() []Provider {
	data, err := os.ReadFile(customFile())
	if err != nil {
		return nil
	}
	var m map[string]CustomProfile
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	var out []Provider
	for name, c := range m {
		out = append(out, profileToProvider(name, c))
	}
	return out
}

func saveCustomProfile(name string, c CustomProfile) {
	f := customFile()
	_ = os.MkdirAll(filepath.Dir(f), 0o700)
	m := map[string]CustomProfile{}
	if data, err := os.ReadFile(f); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	m[name] = c
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(f, b, 0o600); err == nil {
		fmt.Fprintf(os.Stderr, "✅ 已保存为自定义别名 '%s'（出现在 `cld list` 中）\n", name)
	} else {
		fmt.Fprintf(os.Stderr, "⚠️  保存失败: %v\n", err)
	}
}

// profileToProvider 把自定义端点包装成 Provider，复用现有启动路径。
func profileToProvider(name string, c CustomProfile) Provider {
	p := Provider{
		Alias:  name,
		Name:   "自定义 · " + hostOf(c.Base),
		Key:    c.Key,
		CLI:    cliForProtocol(c.Protocol),
		Models: []Model{{ID: c.Model, Tag: name, Latest: true}},
	}
	if c.Protocol == "anthropic" {
		p.ClaudeURL = c.Base
	} else {
		p.OpenAIURL = c.Base
	}
	return p
}

func cliForProtocol(proto string) []string {
	if proto == "anthropic" {
		return []string{"claude", "opencode"}
	}
	return []string{"codex", "opencode"}
}

// runCustom 是 `cld/cdx/opc custom` 的入口：交互输入 → 可用性探测 → 启动（可选保存）。
func runCustom(cli string, skip bool, pass []string) error {
	fmt.Fprintf(os.Stderr, "\n🛠  自定义端点（目标 CLI: %s）\n", cli)
	base := strings.TrimRight(promptLine("端点 base URL: "), "/")
	model := promptLine("model id: ")
	key, err := readHiddenPrompt("API key（输入隐藏）: ")
	if err != nil {
		return fmt.Errorf("读取 key 失败: %w", err)
	}
	protocol := "openai"
	if cli == "claude" {
		protocol = "anthropic"
	} else if cli == "opencode" {
		protocol = promptProtocol()
	}
	if base == "" || model == "" || key == "" {
		return fmt.Errorf("端点 / model / key 均不能为空")
	}

	fmt.Fprintln(os.Stderr, "探测中…")
	ok, msg := probe(protocol, base, model, key)
	fmt.Fprintln(os.Stderr, msg)
	if !ok {
		return fmt.Errorf("可用性检测未通过，已中止")
	}

	// 可选保存为自定义别名（默认不保存）
	fmt.Fprint(os.Stderr, "保存为自定义别名以便下次直接用? [y/N] ")
	if s := promptLine(""); s == "y" || s == "Y" || strings.EqualFold(s, "yes") {
		name := promptLine("别名（如 myapi）: ")
		if name != "" {
			saveCustomProfile(name, CustomProfile{Protocol: protocol, Base: base, Model: model, Key: key})
		}
	}

	p := profileToProvider("custom", CustomProfile{Protocol: protocol, Base: base, Model: model, Key: key})
	p.CLI = []string{cli}
	switch cli {
	case "claude":
		return launchClaude(&p, model, skip, false, pass)
	case "codex":
		return launchCodex(&p, model, skip, false, pass)
	case "opencode":
		return launchOpencode(&p, model, skip, false, pass)
	}
	return nil
}

// probe 对端点发一个最小请求，判断可用性。
// anthropic: POST {base}/v1/messages；openai: POST {base}/chat/completions（404 则试 /v1/）。
func probe(protocol, base, model, key string) (bool, string) {
	client := &http.Client{Timeout: 20 * time.Second}
	body := fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"ping"}]}`, model)

	var urls []string
	headers := map[string]string{"content-type": "application/json"}
	if protocol == "anthropic" {
		urls = []string{base + "/v1/messages"}
		headers["anthropic-version"] = "2023-06-01"
		headers["x-api-key"] = key
		headers["Authorization"] = "Bearer " + key
	} else {
		urls = []string{base + "/chat/completions", base + "/v1/chat/completions"}
		headers["Authorization"] = "Bearer " + key
	}

	var status int
	tried := 0
	for _, u := range urls {
		req, err := http.NewRequest("POST", u, strings.NewReader(body))
		if err != nil {
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			continue // 网络/超时；若有备用 URL 继续试
		}
		status = resp.StatusCode
		resp.Body.Close()
		tried++
		if status == 404 && len(urls) > 1 && u == urls[0] {
			continue // openai：换 /v1/ 重试
		}
		break
	}
	if tried == 0 {
		return false, "✗ 连不上端点（网络错误或超时）"
	}
	return interpretStatus(status)
}

func interpretStatus(code int) (bool, string) {
	switch {
	case code >= 200 && code < 300:
		return true, fmt.Sprintf("✓ 可用（HTTP %d）", code)
	case code == 401 || code == 403:
		return false, fmt.Sprintf("✗ key 无效或无权限（HTTP %d）", code)
	case code == 404:
		return false, "✗ 端点路径不对（HTTP 404）—— 检查 base URL"
	case code == 400 || code == 422:
		return false, fmt.Sprintf("⚠ 鉴权通过但请求被拒（HTTP %d）—— 多半是 model id 不对", code)
	case code == 429:
		return false, "⚠ 触发限流（HTTP 429）—— 端点可用，但请降低频率/检查额度"
	default:
		return false, fmt.Sprintf("✗ 异常状态（HTTP %d）", code)
	}
}

// ---- 交互读取小工具 ----

// promptLine 回显地读一行。
func promptLine(prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	return readLineCooked()
}

// readLineCooked 逐字节读到换行（回显由终端 cooked 模式负责）。
// 不用 bufio，避免与 term.ReadPassword 混用时缓冲预读把后续输入吞掉（管道输入尤其敏感）。
func readLineCooked() string {
	one := make([]byte, 1)
	var buf []byte
	for {
		n, err := os.Stdin.Read(one)
		if n > 0 {
			if one[0] == '\n' || one[0] == '\r' {
				break
			}
			buf = append(buf, one[0])
		}
		if err != nil {
			break
		}
	}
	return strings.TrimSpace(string(buf))
}

// readHiddenPrompt 先打印 prompt，再隐藏读一行（复用 term.ReadPassword）。
func readHiddenPrompt(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	s, err := readHidden()
	fmt.Fprintln(os.Stderr)
	return s, err
}

// promptProtocol 让用户选 openai / anthropic，默认 openai。
func promptProtocol() string {
	fmt.Fprint(os.Stderr, "端点协议 [1]openai（默认，回车） [2]anthropic: ")
	s := promptLine("")
	if s == "2" || strings.Contains(strings.ToLower(s), "anthropic") {
		return "anthropic"
	}
	return "openai"
}
