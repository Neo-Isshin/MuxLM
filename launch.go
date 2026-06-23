package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ---- claude: inline env + exec（不写全局 settings.json）----
func launchClaude(p *Provider, model string, skip, intl bool, pass []string) error {
	key, err := getKey(p, &intl)
	if err != nil {
		return err
	}
	url := p.claudeURL(intl)
	if url == "" {
		return fmt.Errorf("%s 没有 claude(anthropic) 端点", p.Name)
	}
	args := []string{"--model", model}
	if skip {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = append(args, pass...)
	cmd := exec.Command("claude", args...)
	cmd.Env = append(os.Environ(),
		"ANTHROPIC_BASE_URL="+url,
		"ANTHROPIC_AUTH_TOKEN="+key,
	)
	return run(cmd)
}

// ---- codex: 一次性 CODEX_HOME（临时 config.toml + auth.json，跑完即弃）----
func launchCodex(p *Provider, model string, skip, intl bool, pass []string) error {
	key, err := getKey(p, &intl)
	if err != nil {
		return err
	}
	url := p.openaiURL(intl)
	if url == "" {
		return fmt.Errorf("%s 没有 codex(openai) 端点", p.Name)
	}
	dir, err := os.MkdirTemp("", "cx-codex-*")
	if err != nil {
		return err
	}
	ab, _ := json.Marshal(map[string]string{"OPENAI_API_KEY": key})
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), ab, 0o600); err != nil {
		return err
	}
	toml := fmt.Sprintf(`model_provider = "cx"
model = %q
[model_providers.cx]
name = "cx"
base_url = %q
wire_api = %q
`, model, url, p.wireAPI())
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o600); err != nil {
		return err
	}
	var args []string
	if skip {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}
	args = append(args, pass...)
	cmd := exec.Command("codex", args...)
	cmd.Env = append(os.Environ(), "CODEX_HOME="+dir)
	return run(cmd)
}

// ---- opencode: 一次性 OPENCODE_CONFIG_DIR（临时 opencode.json）----
func launchOpencode(p *Provider, model string, skip, intl bool, pass []string) error {
	key, err := getKey(p, &intl)
	if err != nil {
		return err
	}
	url := p.openaiURL(intl)
	npm := "@ai-sdk/openai-compatible"
	if url == "" {
		url = p.claudeURL(intl)
		npm = "@ai-sdk/anthropic"
	}
	if url == "" {
		return fmt.Errorf("%s 没有可用端点", p.Name)
	}
	dir, err := os.MkdirTemp("", "cx-opencode-*")
	if err != nil {
		return err
	}
	cfg := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{
			"cx": map[string]any{
				"npm":  npm,
				"name": "cx",
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
		// opencode 无 --dangerously-bypass flag；通过配置放宽权限 + run --force
		cfg["permission"] = map[string]any{
			"bash": "allow", "edit": "allow", "webfetch": "allow",
		}
	}
	cb, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), cb, 0o600); err != nil {
		return err
	}
	args := []string{"--model", "cx/" + model}
	if skip {
		args = append(args, "--force")
	}
	args = append(args, pass...)
	cmd := exec.Command("opencode", args...)
	cmd.Env = append(os.Environ(), "OPENCODE_CONFIG_DIR="+dir)
	return run(cmd)
}

// run 接管 stdio 并转发退出码。
func run(cmd *exec.Cmd) error {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		return err
	}
	return nil
}

// preview 打印将要执行的命令（--dry-run）。
func preview(cli string, p *Provider, model string, skip, intl bool, pass []string) {
	// key 来源描述：自定义别名是内联 key，其它走 env（按区域）
	var keyDesc string
	if p.Key != "" {
		keyDesc = "key=内联(自定义别名)"
	} else {
		envName := p.keyEnv(intl)
		status := "已设置"
		if loadKeys()[envName] == "" {
			status = "未设置(将交互输入)"
		}
		keyDesc = envName + "=" + status
	}
	fmt.Printf("【DRY-RUN】 %s | %s | model=%s | skip=%v | intl=%v | %s\n",
		cli, p.Name, model, skip, intl, keyDesc)
	switch cli {
	case "claude":
		args := []string{"--model", model}
		if skip {
			args = append(args, "--dangerously-skip-permissions")
		}
		args = append(args, pass...)
		tokenSrc := "$" + p.keyEnv(intl)
		if p.Key != "" {
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
		args := []string{"--model", "cx/" + model}
		if skip {
			args = append(args, "--force")
		}
		args = append(args, pass...)
		url := p.openaiURL(intl)
		npm := "@ai-sdk/openai-compatible"
		if url == "" {
			url = p.claudeURL(intl)
			npm = "@ai-sdk/anthropic"
		}
		fmt.Printf("  env  OPENCODE_CONFIG_DIR=<tmp>  baseURL=%s  npm=%s\n", url, npm)
		fmt.Printf("  run  opencode %s\n", joinArgs(args))
	}
}

func joinArgs(a []string) string { return strings.Join(a, " ") }
