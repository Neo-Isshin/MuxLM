package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/term"
)

type customProviderFile struct {
	Version  int      `json:"version"`
	Provider Provider `json:"provider"`
}

func customProviderPath(id string) string { return filepath.Join(providerDir(id), "provider.json") }

func printConfig(cli string) error {
	global := cli == "claude" || cli == "opencode"
	if global {
		fmt.Println(tr("\nMuxLM 全局配置中心（Anthropic + OpenAI routes）", "\nMuxLM global configuration (Anthropic + OpenAI routes)"))
	} else {
		fmt.Println(tr("\nMuxLM OpenAI-compatible 过滤视图（与 cld config 共享配置）", "\nMuxLM OpenAI-compatible view (shared with cld config)"))
	}
	fmt.Printf("%-12s %-11s %-11s %-14s %-18s %-12s %s\n", "PROVIDER", "ALIAS", "PLAN", "ANTHROPIC", "OPENAI / WIRE", "KEY REGIONS", "KEYS")
	fmt.Println(strings.Repeat("-", 96))
	seen := map[string]bool{}
	all := append([]Provider{}, catalogProviders()...)
	all = append(all, loadCustomProfiles()...)
	for i := range all {
		p := &all[i]
		if !global && p.OpenAIURL == "" && p.OpenAIURLIntl == "" {
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
			if !keyPlanMatches(p, k.Plan) {
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
		fmt.Printf("%-12s %-11s %-11s %-14s %-18s %-12s %d%s\n",
			p.providerID(), p.Alias, p.planID(), routeSummary(p.ClaudeURL, p.ClaudeURLIntl, ""),
			routeSummary(p.OpenAIURL, p.OpenAIURLIntl, p.wireAPI()), region, cn+intl, envStatus)
		if len(names) > 0 {
			fmt.Printf("  key names: %s\n", strings.Join(names, ", "))
		}
	}
	fmt.Printf(tr("\n配置目录: %s\n密钥后端: %s\n界面语言: %s\n", "\nConfiguration: %s\nSecret backend: %s\nInterface language: %s\n"), providersDir(), secretBackend(), uiLanguage())
	if _, err := os.Stat(customFile()); err == nil {
		fmt.Println(tr("⚠ 检测到 v1 custom.json；其中可能含明文 key。新 provider 已使用安全存储，请重新 add 后手动归档/删除旧文件。", "⚠ Found v1 custom.json, which may contain plaintext keys. Re-add providers with secure storage, then archive or remove the old file."))
	}
	return nil
}

func routeSummary(domestic, intl, wire string) string {
	if domestic == "" && intl == "" {
		return "—"
	}
	regions := "cn"
	if domestic == "" {
		regions = "intl"
	} else if intl != "" {
		regions = "cn+intl"
	}
	if wire != "" {
		return wire + " · " + regions
	}
	return regions
}

func runConfig(cli string) error {
	if err := printConfig(cli); err != nil {
		return err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil
	}
	return runConfigMenu(cli)
}

func runConfigMenu(cli string) error {
	global := cli == "claude" || cli == "opencode"
	for {
		fmt.Fprintln(os.Stderr, tr("\n配置操作:", "\nConfiguration actions:"))
		fmt.Fprintln(os.Stderr, tr("  1) 添加 provider / 具名 key", "  1) Add provider / named key"))
		fmt.Fprintln(os.Stderr, tr("  2) 按别名增加 key", "  2) Add a key by alias"))
		fmt.Fprintln(os.Stderr, tr("  3) 删除 provider 本地配置", "  3) Remove local provider configuration"))
		fmt.Fprintln(os.Stderr, tr("  4) 更新模型列表", "  4) Update model catalog"))
		fmt.Fprintln(os.Stderr, tr("  5) 刷新列表", "  5) Refresh list"))
		if cli == "claude" {
			fmt.Fprintln(os.Stderr, "  6) Language / 语言")
		}
		fmt.Fprintln(os.Stderr, tr("  0) 退出", "  0) Exit"))
		switch promptLine(tr("请选择 [0]: ", "Choose [0]: ")) {
		case "", "0":
			return nil
		case "1":
			if err := runAddScoped(cli, global); err != nil {
				fmt.Fprintln(os.Stderr, "✗ "+err.Error())
			}
		case "2":
			alias := promptLine(tr("provider/模型别名: ", "Provider/model alias: "))
			if alias != "" {
				if err := runSetKeyScoped(cli, alias, global); err != nil {
					fmt.Fprintln(os.Stderr, "✗ "+err.Error())
				}
			}
		case "3":
			alias := promptLine(tr("provider/模型别名: ", "Provider/model alias: "))
			if alias != "" {
				if err := runRemoveScoped(alias, cli, global); err != nil {
					fmt.Fprintln(os.Stderr, "✗ "+err.Error())
				}
			}
		case "4":
			if err := runCatalogUpdate(); err != nil {
				fmt.Fprintln(os.Stderr, "✗ "+err.Error())
			}
		case "5":
			if err := printConfig(cli); err != nil {
				return err
			}
		case "6":
			if cli != "claude" {
				fmt.Fprintln(os.Stderr, tr("⚠ 无效选择", "⚠ Invalid choice"))
				continue
			}
			if err := configureLanguage(); err != nil {
				fmt.Fprintln(os.Stderr, "✗ "+err.Error())
			}
		default:
			fmt.Fprintln(os.Stderr, tr("⚠ 无效选择", "⚠ Invalid choice"))
		}
	}
}

func configureLanguage() error {
	fmt.Fprintln(os.Stderr, "\nLanguage / 语言:")
	fmt.Fprintln(os.Stderr, "  1) Auto / 跟随系统")
	fmt.Fprintln(os.Stderr, "  2) English")
	fmt.Fprintln(os.Stderr, "  3) 中文")
	choice := promptLine("Choose / 请选择 [1]: ")
	preference := languageAuto
	switch choice {
	case "", "1":
	case "2":
		preference = languageEN
	case "3":
		preference = languageZH
	default:
		return fmt.Errorf(tr("无效语言选择", "invalid language choice"))
	}
	if err := saveLanguagePreference(preference); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, tr("✓ 语言设置已保存并立即生效", "✓ Language preference saved and applied"))
	return nil
}

func runRemoveScoped(alias, cli string, global bool) error {
	r, ok := buildIndex()[alias]
	if !ok {
		return fmt.Errorf(tr("未知别名: %s", "unknown alias: %s"), alias)
	}
	if !global && cli == "codex" && r.Prov.OpenAIURL == "" && r.Prov.OpenAIURLIntl == "" {
		return fmt.Errorf(tr("%s 不在 OpenAI-compatible 过滤视图中", "%s is not available in the OpenAI-compatible view"), alias)
	}
	return runRemove(alias)
}

func runAdd(cli string) error { return runAddScoped(cli, false) }

func runAddScoped(cli string, global bool) error {
	var choices []Provider
	for _, p := range catalogProviders() {
		if global || p.supports(cli) {
			choices = append(choices, p)
		}
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Alias < choices[j].Alias })
	target := cli
	if global {
		target = tr("全局 Anthropic + OpenAI", "global Anthropic + OpenAI")
	}
	fmt.Fprintf(os.Stderr, tr("\n选择要配置的 provider/套餐（视图 %s）:\n", "\nChoose a provider/plan (view: %s):\n"), target)
	for i, p := range choices {
		fmt.Fprintf(os.Stderr, "  %d) %-10s %s / %s\n", i+1, p.Alias, localizedProviderName(p.Name), planDisplay(p.planID()))
	}
	fmt.Fprintf(os.Stderr, tr("  %d) custom     自定义 provider\n", "  %d) custom     Custom provider\n"), len(choices)+1)
	n, _ := strconv.Atoi(promptLine(tr("请选择（回车取消）: ", "Choose (Enter to cancel): ")))
	if n == 0 {
		return fmt.Errorf(tr("已取消", "cancelled"))
	}
	if n == len(choices)+1 {
		if global {
			return runAddCustom("opencode")
		}
		return runAddCustom(cli)
	}
	if n < 1 || n > len(choices) {
		return fmt.Errorf(tr("无效选择", "invalid choice"))
	}
	p := &choices[n-1]
	validationCLI := cli
	if global {
		validationCLI = chooseValidationCLI(p)
	}
	region := "cn"
	if p.hasIntlFor(validationCLI) && chooseIntl(p, validationCLI) {
		region = "intl"
	}
	model := latestModel(p)
	_, err := addNamedKey(p, region, validationCLI, model)
	return err
}

func runSetKey(cli, alias string) error {
	return runSetKeyScoped(cli, alias, false)
}

func runSetKeyScoped(cli, alias string, global bool) error {
	r, ok := buildIndex()[alias]
	if !ok {
		return fmt.Errorf(tr("未知别名: %s", "unknown alias: %s"), alias)
	}
	if !global && !r.Prov.supports(cli) {
		return fmt.Errorf(tr("%s 不支持 %s", "%s does not support %s"), alias, cli)
	}
	validationCLI := cli
	if global {
		validationCLI = chooseValidationCLI(r.Prov)
	}
	region := "cn"
	if r.Prov.hasIntlFor(validationCLI) && chooseIntl(r.Prov, validationCLI) {
		region = "intl"
	}
	_, err := addNamedKey(r.Prov, region, validationCLI, r.Model.ID)
	return err
}

func chooseValidationCLI(p *Provider) string {
	hasAnthropic := p.ClaudeURL != "" || p.ClaudeURLIntl != ""
	hasOpenAI := p.OpenAIURL != "" || p.OpenAIURLIntl != ""
	if hasAnthropic && hasOpenAI {
		fmt.Fprintln(os.Stderr, tr("选择此 key 的验证 route:", "Choose the validation route for this key:"))
		fmt.Fprintln(os.Stderr, tr("  1) Anthropic-compatible（默认）", "  1) Anthropic-compatible (default)"))
		fmt.Fprintln(os.Stderr, "  2) OpenAI-compatible")
		if promptLine(tr("请选择 [1]: ", "Choose [1]: ")) == "2" {
			return "codex"
		}
		return "claude"
	}
	if hasOpenAI {
		return "codex"
	}
	return "claude"
}

func runAddCustom(cli string) error {
	target := cli
	if cli == "opencode" {
		target = "OpenAI / Anthropic"
	}
	fmt.Fprintf(os.Stderr, tr("\n新增自定义 provider（route %s）\n", "\nAdd custom provider (route: %s)\n"), target)
	alias := strings.ToLower(promptLine(tr("别名: ", "Alias: ")))
	if safeID(alias) != alias || alias == "" {
		return fmt.Errorf(tr("别名只能包含小写字母、数字、点、下划线或短横线", "alias may contain only lowercase letters, digits, dots, underscores, or hyphens"))
	}
	if isReservedAlias(alias) {
		return fmt.Errorf(tr("别名 %q 是保留命令", "alias %q is reserved"), alias)
	}
	if retiredCatalogTags()[alias] {
		return fmt.Errorf(tr("别名 %q 曾用于已退役 catalog 模型，不能复用", "alias %q belonged to a retired catalog model and cannot be reused"), alias)
	}
	if _, exists := buildIndex()[alias]; exists {
		return fmt.Errorf(tr("别名 %q 已存在", "alias %q already exists"), alias)
	}
	protocol := "openai"
	if cli == "claude" {
		protocol = "anthropic"
	} else if cli == "opencode" {
		protocol = promptProtocol()
	}
	base := strings.TrimRight(promptLine(tr("端点 base URL: ", "Endpoint base URL: ")), "/")
	allowInsecure := false
	if err := validateEndpoint(base, false); err != nil {
		if strings.HasPrefix(base, "http://") && strings.ToLower(promptLine(tr("端点不是 HTTPS。输入 insecure 继续（不推荐）: ", "Endpoint is not HTTPS. Enter insecure to continue (not recommended): "))) == "insecure" {
			allowInsecure = true
		} else {
			return err
		}
	}
	if err := validateEndpoint(base, allowInsecure); err != nil {
		return err
	}
	model := promptLine("Model ID: ")
	if model == "" {
		return fmt.Errorf(tr("model 不能为空", "model cannot be empty"))
	}
	id := "custom-" + alias
	p := Provider{ID: id, Alias: alias, Name: tr("自定义 · ", "Custom · ") + hostOf(base), Plan: "custom", KeyEnv: "MUXLM_" + strings.ToUpper(strings.ReplaceAll(alias, "-", "_")) + "_KEY", Models: []Model{{ID: model, Latest: true}}}
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
	fmt.Fprintf(os.Stderr, tr("✓ provider %q 已保存\n", "✓ Provider %q saved\n"), alias)
	return nil
}

func runRemove(alias string) error {
	r, ok := buildIndex()[alias]
	if !ok {
		return fmt.Errorf(tr("未知别名: %s", "unknown alias: %s"), alias)
	}
	id := r.Prov.providerID()
	if strings.ToLower(promptLine(fmt.Sprintf(tr("确认删除 provider %q 的全部已保存 key？输入 yes: ", "Delete all saved keys for provider %q? Enter yes: "), id))) != "yes" {
		return fmt.Errorf(tr("已取消", "cancelled"))
	}
	paths, err := providerRemovalPaths(id)
	if err != nil {
		return err
	}
	keys, err := loadProviderKeys(id)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if err := secretDelete(id, k.Backend, k.Ref); err != nil {
			return fmt.Errorf(tr("删除 key %q 失败: %w", "failed to delete key %q: %w"), k.Name, err)
		}
	}
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, tr("✓ 已删除 %s 的本地配置\n", "✓ Removed local configuration for %s\n"), id)
	return nil
}

// providerRemovalPaths resolves and validates every deletion target before any
// secret or metadata is removed. This prevents a symlinked config/providers
// parent from redirecting RemoveAll outside MuxLM's configuration tree.
func providerRemovalPaths(id string) ([]string, error) {
	if id == "" || safeID(id) != id {
		return nil, fmt.Errorf("非法 provider id")
	}
	var paths []string
	for _, root := range providerDirsForRead() {
		configRoot := filepath.Dir(root)
		configInfo, err := os.Lstat(configRoot)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if configInfo.Mode()&os.ModeSymlink != 0 || !configInfo.IsDir() {
			return nil, fmt.Errorf("拒绝从不安全的配置目录删除: %s", configRoot)
		}
		rootInfo, err := os.Lstat(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
			return nil, fmt.Errorf("拒绝从不安全的 provider 配置目录删除: %s", root)
		}
		path := filepath.Join(root, id)
		pathInfo, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.IsDir() {
			return nil, fmt.Errorf("拒绝删除不安全的 provider 配置路径: %s", path)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func latestModel(p *Provider) string {
	for _, m := range p.Models {
		if m.Latest {
			return m.ID
		}
	}
	return ""
}
