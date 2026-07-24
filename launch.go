package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

// launchDefault starts the selected CLI with its normal account and config.
// MuxLM routing variables are scrubbed so a previous provider override cannot
// shadow the CLI's own login state or default model.
func launchDefault(cli string, skip bool, pass []string) error {
	args := defaultLaunchArgs(cli, skip, pass)
	// #nosec G204 G702 -- cli is one of three fixed executable names and pass
	// contains arguments explicitly supplied by the user.
	cmd := exec.Command(cli, args...)
	cmd.Env = childEnv(nil)
	return run(cmd)
}

func defaultLaunchArgs(cli string, skip bool, pass []string) []string {
	var args []string
	if skip {
		switch cli {
		case "claude":
			args = append(args, "--dangerously-skip-permissions")
		case "codex":
			args = append(args, "--dangerously-bypass-approvals-and-sandbox")
		case "opencode":
			args = append(args, "--auto")
		}
	}
	return append(args, pass...)
}

// ---- claude: inline env + exec（不写全局 settings.json）----
func launchClaude(p *Provider, model string, skip, intl bool, pass []string) error {
	claudeModel, _ := claudeLaunchSettings(p, model, "", "")
	key, err := getKey(p, &intl, "claude", claudeModel)
	if err != nil {
		return err
	}
	url := p.claudeURL(intl)
	if url == "" {
		return fmt.Errorf("%s 没有 claude(anthropic) 端点", p.Name)
	}
	_, env := claudeLaunchSettings(p, model, url, key)
	args := []string{"--model", claudeModel}
	if skip {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = append(args, pass...)
	// #nosec G204 G702 -- 参数传给用户明确请求的底层 CLI，exec.Command 不经过 shell。
	cmd := exec.Command("claude", args...)
	cmd.Env = childEnv(env)
	return run(cmd)
}

func claudeLaunchSettings(p *Provider, model, url, key string) (string, map[string]string) {
	claudeModel := model
	env := map[string]string{
		"ANTHROPIC_BASE_URL":   url,
		"ANTHROPIC_AUTH_TOKEN": key,
	}

	// Moonshot exposes a Claude Code compatibility name for K3.
	if p.providerID() == "kimi" && p.planID() == "standard" && model == "kimi-k3" {
		claudeModel = "kimi-k3[1m]"
	}
	// Keep background summaries and subagents on the selected provider model.
	// This is required for relays too: their catalog may not contain Anthropic's
	// default Haiku/Sonnet/Opus ids.
	for _, name := range []string{
		"ANTHROPIC_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"ANTHROPIC_DEFAULT_FABLE_MODEL",
		"CLAUDE_CODE_SUBAGENT_MODEL",
	} {
		env[name] = claudeModel
	}
	if p.providerID() != "kimi" || (p.planID() != "standard" && p.planID() != "coding") {
		return claudeModel, env
	}
	env["ENABLE_TOOL_SEARCH"] = "false"
	if strings.HasPrefix(claudeModel, "kimi-k3") {
		env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] = "1048576"
		env["CLAUDE_CODE_EFFORT_LEVEL"] = "max"
	} else {
		env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] = "262144"
	}
	return claudeModel, env
}

// ---- codex: 一次性 CODEX_HOME（临时 config.toml + auth.json，跑完即弃）----
func launchCodex(p *Provider, model string, skip, intl bool, pass []string) error {
	key, err := getKey(p, &intl, "codex", model)
	if err != nil {
		return err
	}
	url := p.openaiURL(intl)
	if url == "" {
		return fmt.Errorf("%s 没有 codex(openai) 端点", p.Name)
	}
	dir, err := os.MkdirTemp("", "muxlm-codex-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	ab, _ := json.Marshal(map[string]string{"OPENAI_API_KEY": key})
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), ab, 0o600); err != nil {
		return err
	}
	toml := fmt.Sprintf(`model_provider = "muxlm"
model = %q
[model_providers.muxlm]
name = "MuxLM"
base_url = %q
wire_api = %q
`, model, url, p.wireAPI())
	// #nosec G703 -- dir 由 MkdirTemp 创建，文件名是固定字面量。
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o600); err != nil {
		return err
	}
	var args []string
	if skip {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}
	args = append(args, pass...)
	// #nosec G204 G702 -- 参数传给用户明确请求的底层 CLI，exec.Command 不经过 shell。
	cmd := exec.Command("codex", args...)
	cmd.Env = childEnv(map[string]string{"CODEX_HOME": dir})
	return run(cmd)
}

// ---- opencode: 一次性 OPENCODE_CONFIG_DIR（临时 opencode.json）----
func launchOpencode(p *Provider, model string, skip, intl bool, pass []string) error {
	key, err := getKey(p, &intl, "opencode", model)
	if err != nil {
		return err
	}
	url := p.openaiURL(intl)
	npm := openCodeNPM(p)
	if url == "" {
		url = p.claudeURL(intl)
		npm = "@ai-sdk/anthropic"
	}
	if url == "" {
		return fmt.Errorf("%s 没有可用端点", p.Name)
	}
	dir, err := os.MkdirTemp("", "muxlm-opencode-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	cfg := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{
			"muxlm": map[string]any{
				"npm":  npm,
				"name": appName,
				"options": map[string]any{
					"baseURL": url,
					"apiKey":  key,
				},
				"models": map[string]any{
					model: map[string]any{"name": model},
				},
			},
		},
	}
	if skip {
		// opencode 通过 permission 配置 + --auto 自动批准。
		cfg["permission"] = "allow"
	}
	cb, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), cb, 0o600); err != nil {
		return err
	}
	args := []string{"--model", "muxlm/" + model}
	if skip {
		args = append(args, "--auto")
	}
	args = append(args, pass...)
	// #nosec G204 G702 -- 参数传给用户明确请求的底层 CLI，exec.Command 不经过 shell。
	cmd := exec.Command("opencode", args...)
	cmd.Env = childEnv(map[string]string{"OPENCODE_CONFIG_DIR": dir})
	return run(cmd)
}

// run 接管 stdio 并转发退出码。
func run(cmd *exec.Cmd) error {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Keep the child in our foreground process group so interactive CLIs can
	// continue reading from the terminal. Signals sent only to MuxLM are
	// forwarded once; a second signal is treated as an explicit force-exit.
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	forwarded := false
	for {
		select {
		case err := <-done:
			return normalizeCommandError(err)
		case received := <-signals:
			if !forwarded {
				forwarded = true
				// A terminal usually signals the whole foreground process group;
				// forwarding also covers signals sent only to MuxLM itself.
				_ = cmd.Process.Signal(received)
				continue
			}
			// Do not swallow repeated Ctrl-C/termination requests when a child
			// ignores the graceful signal. Wait still runs so caller defers can
			// remove temporary configs before MuxLM exits.
			_ = cmd.Process.Kill()
		}
	}
}

type commandExitError struct {
	cause error
	code  int
}

func (e *commandExitError) Error() string { return e.cause.Error() }
func (e *commandExitError) Unwrap() error { return e.cause }
func (e *commandExitError) ExitCode() int { return e.code }

func normalizeCommandError(err error) error {
	if err == nil {
		return nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return &commandExitError{cause: err, code: 128 + int(status.Signal())}
		}
	}
	return err
}

// preview 打印将要执行的命令（--dry-run）。
func preview(cli string, p *Provider, model string, skip, intl bool, pass []string) {
	// key 来源描述：自定义别名是内联 key，其它走 env（按区域）
	var keyDesc string
	if p.Key != "" {
		keyDesc = "内联（旧版自定义配置）"
	} else {
		envName := p.keyEnv(intl)
		status := configuredKeyStatus(p, intl)
		keyDesc = envName + ": " + status
	}
	region := "cn"
	if intl {
		region = "intl"
	}
	mode := region
	if skip {
		mode += ", unsafe"
	}
	fmt.Printf("DRY RUN  %s → %s / %s [%s]\n", cli, p.Name, model, mode)
	fmt.Printf("key      %s\n", keyDesc)
	switch cli {
	case "claude":
		claudeModel, _ := claudeLaunchSettings(p, model, p.claudeURL(intl), "")
		args := []string{"--model", claudeModel}
		if skip {
			args = append(args, "--dangerously-skip-permissions")
		}
		args = append(args, pass...)
		tokenSrc := "$" + p.keyEnv(intl)
		if p.Key != "" {
			// #nosec G101 -- 仅为脱敏状态标签，不是凭据。
			tokenSrc = "(内联)"
		}
		fmt.Printf("  env  ANTHROPIC_BASE_URL=%s  ANTHROPIC_AUTH_TOKEN=%s\n", p.claudeURL(intl), tokenSrc)
		fmt.Printf("  run  claude %s\n", joinArgs(args))
	case "codex":
		var args []string
		if skip {
			args = append(args, "--dangerously-bypass-approvals-and-sandbox")
		}
		args = append(args, pass...)
		fmt.Printf("  env  CODEX_HOME=<tmp>  base_url=%s  wire_api=%s\n", p.openaiURL(intl), p.wireAPI())
		fmt.Printf("  run  codex %s\n", joinArgs(args))
	case "opencode":
		args := []string{"--model", "muxlm/" + model}
		if skip {
			args = append(args, "--auto")
		}
		args = append(args, pass...)
		url := p.openaiURL(intl)
		npm := openCodeNPM(p)
		if url == "" {
			url = p.claudeURL(intl)
			npm = "@ai-sdk/anthropic"
		}
		fmt.Printf("  env  OPENCODE_CONFIG_DIR=<tmp>  baseURL=%s  npm=%s\n", url, npm)
		fmt.Printf("  run  opencode %s\n", joinArgs(args))
	}
}

func previewDefault(cli string, skip bool, pass []string) {
	fmt.Printf("DRY RUN  %s → 默认账号 / 默认模型\n", cli)
	args := joinArgs(defaultLaunchArgs(cli, skip, pass))
	if args == "" {
		fmt.Printf("  run  %s\n", cli)
		return
	}
	fmt.Printf("  run  %s %s\n", cli, args)
}

func openCodeNPM(p *Provider) string {
	if p.openaiURL(false) != "" && p.wireAPI() == "responses" {
		return "@ai-sdk/openai"
	}
	return "@ai-sdk/openai-compatible"
}

func joinArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, quoteArg(arg))
	}
	return strings.Join(quoted, " ")
}

func quoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	for _, r := range arg {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_@%+=:,./-", r) {
			continue
		}
		return "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
	}
	return arg
}

func configuredKeyStatus(p *Provider, intl bool) string {
	region := "cn"
	if intl {
		region = "intl"
	}
	c, err := keyCandidates(p, region)
	if err != nil || len(c) == 0 {
		return "未设置(将交互输入)"
	}
	return fmt.Sprintf("已设置(%d个)", len(c))
}

// childEnv 只传递当前 provider 需要的密钥，避免底层 CLI/插件继承其他 provider key。
func childEnv(extra map[string]string) []string {
	blocked := map[string]bool{
		"ANTHROPIC_AUTH_TOKEN": true, "ANTHROPIC_API_KEY": true, "ANTHROPIC_BASE_URL": true,
		"ANTHROPIC_MODEL": true, "ANTHROPIC_SMALL_FAST_MODEL": true,
		"ANTHROPIC_DEFAULT_OPUS_MODEL": true, "ANTHROPIC_DEFAULT_OPUS_MODEL_NAME": true,
		"ANTHROPIC_DEFAULT_SONNET_MODEL": true, "ANTHROPIC_DEFAULT_SONNET_MODEL_NAME": true,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": true, "ANTHROPIC_DEFAULT_HAIKU_MODEL_NAME": true,
		"ANTHROPIC_DEFAULT_FABLE_MODEL": true, "ANTHROPIC_DEFAULT_FABLE_MODEL_NAME": true,
		"CLAUDE_CODE_SUBAGENT_MODEL": true, "CLAUDE_CODE_AUTO_COMPACT_WINDOW": true,
		"CLAUDE_CODE_EFFORT_LEVEL": true, "ENABLE_TOOL_SEARCH": true,
		"CLAUDE_CONFIG_DIR": true, "CLAUDE_CODE_USE_BEDROCK": true,
		"CLAUDE_CODE_USE_VERTEX": true, "CLAUDE_CODE_USE_FOUNDRY": true,
		"OPENAI_API_KEY": true, "OPENAI_BASE_URL": true, "OPENAI_API_BASE": true,
		"CODEX_HOME": true, "OPENCODE_CONFIG_DIR": true,
	}
	for name := range extra {
		blocked[name] = true
	}
	// Always retain the built-in list as a scrub set. A remote catalog may
	// remove a provider, but that must never make its old key visible again.
	all := append([]Provider{}, providers...)
	all = append(all, catalogProviders()...)
	all = append(all, loadCustomProfiles()...)
	for _, p := range all {
		blocked[p.KeyEnv] = true
		blocked[p.KeyEnv+"_INTL"] = true
	}
	for name := range loadLegacyKeys() {
		blocked[name] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		providerNamespace := strings.HasPrefix(name, "MUXLM_PROVIDER_") || strings.HasPrefix(name, "PROVIDERDECK_PROVIDER_") || strings.HasPrefix(name, "CX_PROVIDER_")
		customKey := (strings.HasPrefix(name, "MUXLM_") || strings.HasPrefix(name, "PROVIDERDECK_") || strings.HasPrefix(name, "CX_")) &&
			(strings.HasSuffix(name, "_KEY") || strings.HasSuffix(name, "_KEY_INTL"))
		if providerNamespace || customKey {
			continue
		}
		if !blocked[name] {
			env = append(env, kv)
		}
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
