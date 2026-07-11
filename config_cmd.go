package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type customProviderFile struct {
	Version  int      `json:"version"`
	Provider Provider `json:"provider"`
}

func customProviderPath(id string) string { return filepath.Join(providerDir(id), "provider.json") }

func printConfig(cli string) error {
	fmt.Printf("\n%-12s %-12s %-12s %-9s %-10s %s\n", "PROVIDER", "ALIAS", "PLAN", "REGION", "KEYS", "ENDPOINT")
	fmt.Println(strings.Repeat("-", 92))
	seen := map[string]bool{}
	all := append([]Provider{}, catalogProviders()...)
	all = append(all, loadCustomProfiles()...)
	for i := range all {
		p := &all[i]
		if !p.supports(cli) {
			continue
		}
		key := p.providerID() + "/" + p.planID()
		if seen[key] {
			continue
		}
		seen[key] = true
		keys, err := loadProviderKeys(p.providerID())
		if err != nil {
			return err
		}
		cn, intl, names := 0, 0, []string{}
		for _, k := range keys {
			if k.Plan != p.planID() {
				continue
			}
			if k.Region == "intl" {
				intl++
			} else {
				cn++
			}
			names = append(names, k.Name+"["+k.Backend+"]")
		}
		envStatus := ""
		if os.Getenv(p.KeyEnv) != "" {
			envStatus = " +env"
		}
		if loadLegacyKeys()[p.KeyEnv] != "" {
			envStatus += " +legacy"
		}
		if p.hasIntl() && os.Getenv(p.KeyEnv+"_INTL") != "" {
			envStatus += " +env-intl"
		}
		if p.hasIntl() && loadLegacyKeys()[p.KeyEnv+"_INTL"] != "" {
			envStatus += " +legacy-intl"
		}
		if p.Key != "" {
			envStatus += " +legacy-custom"
		}
		region := fmt.Sprintf("cn:%d", cn)
		if p.hasIntl() {
			region += fmt.Sprintf("/intl:%d", intl)
		}
		endpoint := p.ClaudeURL
		if cli != "claude" && p.OpenAIURL != "" {
			endpoint = p.OpenAIURL
		}
		fmt.Printf("%-12s %-12s %-12s %-9s %-10s %s\n", p.providerID(), p.Alias, p.planID(), region, fmt.Sprintf("%d%s", cn+intl, envStatus), hostOf(endpoint))
		if len(names) > 0 {
			fmt.Printf("  key names: %s\n", strings.Join(names, ", "))
		}
	}
	fmt.Printf("\n配置目录: %s\n密钥后端: %s\n", providersDir(), secretBackend())
	if _, err := os.Stat(customFile()); err == nil {
		fmt.Println("⚠ 检测到 v1 custom.json；其中可能含明文 key。新 provider 已使用安全存储，请重新 add 后手动归档/删除旧文件。")
	}
	return nil
}

func runAdd(cli string) error {
	var choices []Provider
	for _, p := range catalogProviders() {
		if p.supports(cli) {
			choices = append(choices, p)
		}
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Alias < choices[j].Alias })
	fmt.Fprintf(os.Stderr, "\n选择要配置的 provider/套餐（目标 %s）:\n", cli)
	for i, p := range choices {
		fmt.Fprintf(os.Stderr, "  %d) %-10s %s / %s\n", i+1, p.Alias, p.Name, planDisplay(p.planID()))
	}
	fmt.Fprintf(os.Stderr, "  %d) custom     自定义 provider\n", len(choices)+1)
	n, _ := strconv.Atoi(promptLine("请选择（回车取消）: "))
	if n == 0 {
		return fmt.Errorf("已取消")
	}
	if n == len(choices)+1 {
		return runAddCustom(cli)
	}
	if n < 1 || n > len(choices) {
		return fmt.Errorf("无效选择")
	}
	p := &choices[n-1]
	region := "cn"
	if p.hasIntl() && chooseIntl(p) {
		region = "intl"
	}
	model := latestModel(p)
	_, err := addNamedKey(p, region, cli, model)
	return err
}

func runSetKey(cli, alias string) error {
	r, ok := buildIndex()[alias]
	if !ok {
		return fmt.Errorf("未知别名: %s", alias)
	}
	if !r.Prov.supports(cli) {
		return fmt.Errorf("%s 不支持 %s", alias, cli)
	}
	region := "cn"
	if r.Prov.hasIntl() && chooseIntl(r.Prov) {
		region = "intl"
	}
	_, err := addNamedKey(r.Prov, region, cli, r.Model.ID)
	return err
}

func runAddCustom(cli string) error {
	fmt.Fprintf(os.Stderr, "\n新增自定义 provider（目标 %s）\n", cli)
	alias := strings.ToLower(promptLine("别名: "))
	if safeID(alias) != alias || alias == "" {
		return fmt.Errorf("别名只能包含小写字母、数字、点、下划线或短横线")
	}
	reserved := map[string]bool{"config": true, "add": true, "set-key": true, "remove": true, "update": true, "list": true, "custom": true}
	if reserved[alias] {
		return fmt.Errorf("别名 %q 是保留命令", alias)
	}
	if _, exists := buildIndex()[alias]; exists {
		return fmt.Errorf("别名 %q 已存在", alias)
	}
	protocol := "openai"
	if cli == "claude" {
		protocol = "anthropic"
	} else if cli == "opencode" {
		protocol = promptProtocol()
	}
	base := strings.TrimRight(promptLine("端点 base URL: "), "/")
	allowInsecure := false
	if err := validateEndpoint(base, false); err != nil {
		if strings.HasPrefix(base, "http://") && strings.ToLower(promptLine("端点不是 HTTPS。输入 insecure 继续（不推荐）: ")) == "insecure" {
			allowInsecure = true
		} else {
			return err
		}
	}
	if err := validateEndpoint(base, allowInsecure); err != nil {
		return err
	}
	model := promptLine("model id: ")
	if model == "" {
		return fmt.Errorf("model 不能为空")
	}
	id := "custom-" + alias
	p := Provider{ID: id, Alias: alias, Name: "自定义 · " + hostOf(base), Plan: "custom", KeyEnv: "CX_" + strings.ToUpper(strings.ReplaceAll(alias, "-", "_")) + "_KEY", Models: []Model{{ID: model, Latest: true}}}
	if protocol == "anthropic" {
		p.ClaudeURL = base
		p.CLI = []string{"claude", "opencode"}
	} else {
		p.OpenAIURL = base
		p.CLI = []string{"codex", "opencode"}
	}
	if err := atomicWriteJSON(customProviderPath(id), customProviderFile{Version: 1, Provider: p}); err != nil {
		return err
	}
	if _, err := addNamedKey(&p, "cn", cli, model); err != nil {
		_ = os.Remove(customProviderPath(id))
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ provider %q 已保存\n", alias)
	return nil
}

func runRemove(alias string) error {
	r, ok := buildIndex()[alias]
	if !ok {
		return fmt.Errorf("未知别名: %s", alias)
	}
	id := r.Prov.providerID()
	if strings.ToLower(promptLine(fmt.Sprintf("确认删除 provider %q 的全部已保存 key？输入 yes: ", id))) != "yes" {
		return fmt.Errorf("已取消")
	}
	keys, err := loadProviderKeys(id)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if err := secretDelete(id, k.Backend, k.Ref); err != nil {
			return fmt.Errorf("删除 key %q 失败: %w", k.Name, err)
		}
	}
	path := providerDir(id)
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("拒绝删除符号链接目录")
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ 已删除 %s 的本地配置\n", id)
	return nil
}

func latestModel(p *Provider) string {
	for _, m := range p.Models {
		if m.Latest {
			return m.ID
		}
	}
	return ""
}
