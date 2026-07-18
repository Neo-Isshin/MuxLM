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

// CustomProfile 是用户自定义保存的端点（旧版 custom.json，权限 600）。
type CustomProfile struct {
	Protocol string // "anthropic" | "openai"
	Base     string // 端点 base URL
	Model    string // 模型 id
	Key      string // API key（明文存储，文件权限 600）
}

func customFile() string {
	return filepath.Join(configDir(), "custom.json")
}

// loadCustomProfiles 读 custom.json，转成可挂进索引的 Provider 列表。
func loadCustomProfiles() []Provider {
	var out []Provider
	seen := map[string]bool{}
	seenDirs := map[string]bool{}
	for _, root := range providerDirsForRead() {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if !entry.IsDir() || seenDirs[entry.Name()] {
				continue
			}
			seenDirs[entry.Name()] = true
			data, err := readPrivateFile(filepath.Join(root, entry.Name(), "provider.json"))
			if err != nil {
				continue
			}
			var f customProviderFile
			if json.Unmarshal(data, &f) == nil && f.Version == 1 && validateStoredCustomProvider(&f.Provider, entry.Name()) == nil {
				out = append(out, f.Provider)
				seen[f.Provider.Alias] = true
			} else {
				fmt.Fprintf(os.Stderr, "⚠ 忽略无效自定义 provider: %s\n", entry.Name())
			}
		}
	}
	// v1 兼容：旧 custom.json 仍可使用，但新增 provider 不再将 key 内联写入它。
	data, err := readPrivateFile(customFile())
	if err != nil {
		return out
	}
	var m map[string]CustomProfile
	if err := json.Unmarshal(data, &m); err != nil {
		return out
	}
	for name, c := range m {
		if seen[name] {
			continue
		}
		out = append(out, profileToProvider(name, c))
	}
	return out
}

func validateStoredCustomProvider(p *Provider, dirName string) error {
	if p.ID != dirName || !strings.HasPrefix(p.ID, "custom-") || safeID(p.ID) != p.ID {
		return fmt.Errorf("非法 provider id")
	}
	if p.Alias == "" || safeID(p.Alias) != p.Alias || p.Plan != "custom" {
		return fmt.Errorf("非法 alias/plan")
	}
	if !validEnvName(p.KeyEnv) || p.Key != "" {
		return fmt.Errorf("非法 key 元数据")
	}
	if len(p.Models) != 1 || p.Models[0].ID == "" || !p.Models[0].Latest {
		return fmt.Errorf("自定义 provider 必须有一个 latest model")
	}
	endpoints := 0
	for _, endpoint := range []string{p.ClaudeURL, p.OpenAIURL} {
		if endpoint == "" {
			continue
		}
		endpoints++
		if err := validateEndpoint(endpoint, true); err != nil {
			return err
		}
	}
	if endpoints != 1 {
		return fmt.Errorf("自定义 provider 必须有一个端点")
	}
	for _, cli := range p.CLI {
		if cli != "claude" && cli != "codex" && cli != "opencode" {
			return fmt.Errorf("非法 CLI")
		}
	}
	return nil
}

func validEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if !(r >= 'A' && r <= 'Z' || r == '_' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// profileToProvider 把自定义端点包装成 Provider，复用现有启动路径。
func profileToProvider(name string, c CustomProfile) Provider {
	p := Provider{
		ID:     "custom-" + safeID(name),
		Alias:  name,
		Name:   "自定义 · " + hostOf(c.Base),
		Plan:   "custom",
		KeyEnv: "PROVIDERDECK_" + strings.ToUpper(strings.ReplaceAll(safeID(name), "-", "_")) + "_KEY",
		Key:    c.Key,
		CLI:    cliForProtocol(c.Protocol),
		// 单模型，无需版本别名——用裸别名 <name> 即可，避免表格里 版本别名 与 别名 重复。
		Models: []Model{{ID: c.Model, Tag: "", Latest: true}},
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

// runCustom 保留 v1 命令兼容，新实现统一转到 add。
func runCustom(cli string, skip bool, pass []string) error {
	fmt.Fprintln(os.Stderr, "`custom` 已合并到 `add`；本次只保存 provider，不会自动启动底层 CLI。")
	return runAddCustom(cli)
}

// probe 发一个最小请求探测端点，返回 reachable（是否收到 HTTP 响应）、HTTP code、人读说明。
// 不在此判定「是否通过」——custom（严格 2xx）与 built-in key 校验（仅 401/403 算 key 错）标准不同，由调用方决定。
// anthropic: POST {base}/v1/messages；openai: POST {base}/chat/completions（404 则试 /v1/）。
func probe(protocol, base, model, key string) (reachable bool, code int, msg string) {
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("端点重定向过多")
			}
			origin := via[0].URL
			if !strings.EqualFold(req.URL.Host, origin.Host) {
				return fmt.Errorf("拒绝跨域端点重定向")
			}
			if origin.Scheme == "https" && req.URL.Scheme != "https" {
				return fmt.Errorf("拒绝 HTTPS 降级重定向")
			}
			return nil
		},
	}
	body := fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"ping"}]}`, model)

	var urls []string
	headers := map[string]string{"content-type": "application/json"}
	if protocol == "anthropic" {
		urls = []string{base + "/v1/messages"}
		headers["anthropic-version"] = "2023-06-01"
		headers["x-api-key"] = key
		headers["Authorization"] = "Bearer " + key
	} else if protocol == "responses" {
		body = fmt.Sprintf(`{"model":%q,"input":"ping","max_output_tokens":1}`, model)
		urls = []string{base + "/responses"}
		headers["Authorization"] = "Bearer " + key
	} else {
		urls = []string{base + "/chat/completions", base + "/v1/chat/completions"}
		headers["Authorization"] = "Bearer " + key
	}

	for _, u := range urls {
		req, err := http.NewRequest("POST", u, strings.NewReader(body))
		if err != nil {
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		// #nosec G704 -- 这是本地 CLI 用户明确配置的 provider 端点，非远程服务器代理入参。
		resp, err := client.Do(req)
		if err != nil {
			if resp != nil {
				_ = resp.Body.Close()
			}
			continue // 网络/超时；若有备用 URL 继续试
		}
		code = resp.StatusCode
		_ = resp.Body.Close()
		reachable = true
		if code == 404 && len(urls) > 1 && u == urls[0] {
			continue // openai：换 /v1/ 重试
		}
		break
	}
	msg = statusMsg(reachable, code)
	return
}

// statusMsg 把 可达性 + HTTP 状态码 翻成人读说明（不判定通过与否）。
func statusMsg(reachable bool, code int) string {
	if !reachable {
		return "✗ 连不上端点（网络错误或超时）"
	}
	switch {
	case code >= 200 && code < 300:
		return fmt.Sprintf("✓ 可用（HTTP %d）", code)
	case code == 401 || code == 403:
		return fmt.Sprintf("✗ key 无效或无权限（HTTP %d）", code)
	case code == 404:
		return "✗ 端点路径不对（HTTP 404）—— 检查 base URL"
	case code == 400 || code == 422:
		return fmt.Sprintf("⚠ 鉴权通过但请求被拒（HTTP %d）—— 多半是 model id 不对", code)
	case code == 429:
		return "⚠ 触发限流（HTTP 429）—— 端点可用，但请降低频率/检查额度"
	default:
		return fmt.Sprintf("✗ 异常状态（HTTP %d）", code)
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
