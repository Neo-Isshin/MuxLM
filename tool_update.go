package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type updateMode uint8

const (
	updateCatalog updateMode = iota
	updateTools
	updateSelf
	updateAll
)

type updateRunners struct {
	catalog func() error
	tools   func() error
	self    func() error
}

func parseUpdateMode(args []string) (updateMode, error) {
	if len(args) == 0 {
		return updateCatalog, nil
	}
	if len(args) != 1 {
		return 0, fmt.Errorf("update 一次只能使用 --tool、--self 或 --all 中的一个")
	}
	switch args[0] {
	case "--tool":
		return updateTools, nil
	case "--self":
		return updateSelf, nil
	case "--all":
		return updateAll, nil
	default:
		return 0, fmt.Errorf("update 不支持参数 %q；可用参数: --tool、--self、--all", args[0])
	}
}

func runUpdateCommand(args []string) error {
	mode, err := parseUpdateMode(args)
	if err != nil {
		return err
	}
	return executeUpdate(mode, updateRunners{
		catalog: runCatalogUpdateCommand,
		tools:   runToolUpdates,
		self:    runSelfUpdate,
	})
}

func executeUpdate(mode updateMode, runners updateRunners) error {
	switch mode {
	case updateCatalog:
		return runners.catalog()
	case updateTools:
		return runners.tools()
	case updateSelf:
		return runners.self()
	case updateAll:
		steps := []struct {
			name  string
			title string
			run   func() error
		}{
			{"模型列表", "更新模型列表", runners.catalog},
			{"Codex、Claude Code 和 OpenCode", "更新 Codex、Claude Code 和 OpenCode", runners.tools},
			{"MuxLM", "更新 MuxLM", runners.self},
		}
		var failures []string
		for i, step := range steps {
			fmt.Printf("\n[%d/%d] %s\n", i+1, len(steps), step.title)
			if err := step.run(); err != nil {
				failures = append(failures, step.name+": "+err.Error())
			}
		}
		if len(failures) > 0 {
			return fmt.Errorf("有些内容没有更新成功：%s", strings.Join(failures, "；"))
		}
		return nil
	default:
		return fmt.Errorf("未知 update 模式")
	}
}

func runCatalogUpdateCommand() error {
	_ = recordStartupUpdateAttempt(time.Now())
	return runCatalogUpdate()
}

type toolUpdater struct {
	name  string
	label string
	args  []string
}

// Each supported CLI owns installation-source detection. In particular, their
// updater can preserve npm/Homebrew/native installations without MuxLM having
// to guess from a symlink or a PATH layout that package managers may change.
func runToolUpdates() error {
	updaters := []toolUpdater{
		{name: "codex", label: "Codex", args: []string{"update"}},
		{name: "claude", label: "Claude Code", args: []string{"update"}},
		{name: "opencode", label: "OpenCode", args: []string{"upgrade"}},
	}
	found := 0
	var failures []string
	for _, updater := range updaters {
		path, err := exec.LookPath(updater.name)
		if err != nil {
			fmt.Printf("- 未安装 %s，已跳过\n", updater.label)
			continue
		}
		found++
		if err := verifyUpdaterSubcommand(path, updater.args[0]); err != nil {
			failures = append(failures, updater.label)
			fmt.Printf("✗ %s：%s\n", updater.label, err)
			continue
		}
		fmt.Printf("→ 正在更新 %s…\n", updater.label)
		// #nosec G204 -- path comes from exec.LookPath for a fixed allowlist and
		// arguments are fixed official updater subcommands.
		cmd := exec.Command(path, updater.args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			failures = append(failures, updater.label)
			fmt.Printf("✗ %s 更新失败：%v\n", updater.label, err)
			continue
		}
		fmt.Printf("✓ %s 更新完成\n", updater.label)
	}
	if found == 0 {
		fmt.Println("没有找到 Codex、Claude Code 或 OpenCode，无需更新。")
		return nil
	}
	if len(failures) > 0 {
		return fmt.Errorf("以下程序未能更新：%s", strings.Join(failures, "、"))
	}
	return nil
}

// Older releases of some tools treated an unknown first argument as an
// interactive prompt or project path. Verify the updater appears in local help
// before invoking it so `cld update --tool` can never accidentally open a TUI.
func verifyUpdaterSubcommand(path, subcommand string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// #nosec G204 -- path comes from exec.LookPath for the fixed tool allowlist.
	out, err := exec.CommandContext(ctx, path, "--help").CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("检查更新方式超时")
		}
		return fmt.Errorf("无法确认更新方式：%w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if updaterTokenMatches(fields[0], subcommand) {
			return nil
		}
		binary := strings.TrimSuffix(filepath.Base(path), ".exe")
		if len(fields) > 1 && strings.TrimSuffix(fields[0], ".exe") == binary && updaterTokenMatches(fields[1], subcommand) {
			return nil
		}
	}
	return fmt.Errorf("当前版本太旧，无法自动更新；请按原来的方式升级一次")
}

func updaterTokenMatches(token, subcommand string) bool {
	for _, candidate := range strings.Split(token, "|") {
		if candidate == subcommand {
			return true
		}
	}
	return false
}
