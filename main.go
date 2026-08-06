package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const helpTextZH = appName + ` — 快速切换 Claude Code / Codex / OpenCode 的 provider 与模型

用法:
  cdx|cld|opc def [选项] [-- <底层 CLI 参数...>]
  cdx|cld|opc <模型短名> [选项] [-- <底层 CLI 参数...>]
  cdx|cld|opc <来源短名> [模型短名] [选项] [-- <底层 CLI 参数...>]

入口:
  cdx <别名>   使用 Codex
  cld <别名>   使用 Claude Code
  opc <别名>   使用 OpenCode

命令:
  <入口> def                  使用原生账号与默认模型
  <入口> list                 查看 provider / 模型别名
  <入口> doctor               检查模型列表、配置和依赖程序
  <入口> audit-probes         用假 key 探测 catalog 中所有端点
  <入口> config               查看和管理 provider / key
  <入口> add                  添加 provider 或具名 key
  <入口> set-key <别名>       增加一把具名 key
  <入口> remove <别名>        删除本地 provider 配置
  <入口> update               更新模型列表
  <入口> update --tool        更新已检测到的 Codex / Claude Code / OpenCode
  <入口> update --self        更新 MuxLM
  <入口> update --all         全部更新
  <入口> version              显示 MuxLM 和模型列表版本

选项:
  -m, --model <id>            临时指定模型 id
      --intl                  使用海外端点
  -y, --yes                   跳过底层 CLI 安全确认（危险）
      --dry-run               仅预览，不启动底层 CLI
  -h, --help                  显示帮助

示例:
  cld def                     Claude Code 原生订阅与默认模型
  cdx def                     Codex 原生账号与默认模型
  opc def                     OpenCode 原生配置与默认模型
  cld k3                      Claude Code + Kimi 官方 K3
  cld sf k27                  Claude Code + SiliconFlow 的 Kimi K2.7
  cld glm                     Claude Code + GLM 最新模型
  cld qc                      Claude Code + 百炼 Coding Plan
  cdx q                       Codex + 千问最新模型
  opc or                      OpenCode + OpenRouter
  cdx m --intl                Codex + MiniMax 海外端点
  opc ds -m deepseek-v4-pro   OpenCode + 指定模型
  cld glm -- "fix the bug"    将 -- 后的参数原样传给 Claude Code

def 不使用 MuxLM provider，直接回到对应 CLI 的原生账号、配置与默认模型。
直接使用模型短名时走官方来源；指定来源后，只在该来源中选择模型。
来源短名始终使用该来源的最新模型；原有版本别名仍对应固定模型。
`

const helpTextEN = appName + ` — Quickly switch providers and models for Claude Code, Codex, and OpenCode

Usage:
  cdx|cld|opc def [options] [-- <underlying CLI arguments...>]
  cdx|cld|opc <model shortcut> [options] [-- <underlying CLI arguments...>]
  cdx|cld|opc <source shortcut> [model shortcut] [options] [-- <underlying CLI arguments...>]

Entry points:
  cdx <alias>   Use Codex
  cld <alias>   Use Claude Code
  opc <alias>   Use OpenCode

Commands:
  <entry> def                  Use the native account and default model
  <entry> list                 List providers and model aliases
  <entry> doctor               Check the catalog, configuration, and dependencies
  <entry> audit-probes         Probe all catalog endpoints with a dummy key
  <entry> config               View and manage providers, keys, and language
  <entry> add                  Add a provider or named key
  <entry> set-key <alias>      Add another named key
  <entry> remove <alias>       Remove local provider configuration
  <entry> update               Update the model catalog
  <entry> update --tool        Update detected Codex / Claude Code / OpenCode tools
  <entry> update --self        Update MuxLM
  <entry> update --all         Update everything
  <entry> version              Show MuxLM and catalog versions

Options:
  -m, --model <id>            Temporarily override the model ID
      --intl                  Use the international endpoint
  -y, --yes                   Skip the underlying CLI safety confirmation (dangerous)
      --dry-run               Preview without launching the underlying CLI
  -h, --help                  Show help

Examples:
  cld def                     Claude Code native subscription and default model
  cdx def                     Codex native account and default model
  opc def                     OpenCode native configuration and default model
  cld k3                      Claude Code + official Kimi K3
  cld sf k27                  Claude Code + Kimi K2.7 through SiliconFlow
  cld glm                     Claude Code + latest GLM
  cld qc                      Claude Code + Bailian Coding Plan
  cdx q                       Codex + latest Qwen
  opc or                      OpenCode + OpenRouter
  cdx m --intl                Codex + MiniMax international endpoint
  opc ds -m deepseek-v4-pro   OpenCode + a specific model
  cld glm -- "fix the bug"    Pass arguments after -- to Claude Code

def bypasses MuxLM providers and uses the selected CLI's native account, configuration, and default model.
A model shortcut uses its official source. After a source shortcut, models are resolved only within that source.
A source shortcut follows that source's latest model; existing version aliases remain pinned.
`

func helpText() string { return tr(helpTextZH, helpTextEN) }

func main() {
	prog := filepath.Base(os.Args[0])
	args := os.Args[1:]
	cli := ""
	switch prog {
	case "cdx":
		cli = "codex"
	case "cld":
		cli = "claude"
	case "opc":
		cli = "opencode"
	}

	if len(args) > 0 && args[0] == "update" {
		if err := runUpdateCommand(args[1:]); err != nil {
			fail(err.Error())
		}
		return
	}
	// doctor 必须保持纯本地、只读，因此在启动更新门之前处理。
	if len(args) > 0 && args[0] == "doctor" {
		if len(args) != 1 {
			fail(tr("doctor 不接受额外参数", "doctor does not accept additional arguments"))
		}
		if err := runDoctor(os.Stdout); err != nil {
			fail(err.Error())
		}
		return
	}
	if len(args) > 0 && args[0] == "audit-probes" {
		if err := runAuditProbes(args[1:], os.Stdout); err != nil {
			fail(err.Error())
		}
		return
	}

	if len(args) == 0 || args[0] != "def" {
		checkUpdatesOnStartup()
	}

	if len(args) > 0 && (args[0] == "version" || args[0] == "--version") {
		if len(args) != 1 {
			fail(tr("version 不接受额外参数", "version does not accept additional arguments"))
		}
		printVersion()
		return
	}
	if len(args) == 0 {
		if cli == "" {
			fmt.Print(helpText())
		} else {
			printQuickStart(prog, cli)
		}
		return
	}
	if isHelpCommand(args) {
		fmt.Print(helpText())
		return
	}
	if args[0] == "list" || args[0] == "ls" {
		if len(args) != 1 {
			fail(tr("list 不接受额外参数", "list does not accept additional arguments"))
		}
		printTable()
		return
	}

	// argv[0] 决定目标 CLI：cdx→codex, cld→claude, opc→opencode
	if cli == "" {
		fail(tr("muxlm 不能直接启动模型；请使用 cdx / cld / opc\n例如: cdx glm", "muxlm cannot launch a model directly; use cdx / cld / opc\nExample: cdx glm"))
	}

	if len(args) > 0 {
		switch args[0] {
		case "config":
			if len(args) != 1 {
				fail(tr("config 不接受额外参数", "config does not accept additional arguments"))
			}
			if err := runConfig(cli); err != nil {
				fail(err.Error())
			}
			return
		case "add":
			if len(args) != 1 {
				fail(tr("add 不接受额外参数", "add does not accept additional arguments"))
			}
			if err := runAdd(cli); err != nil {
				fail(err.Error())
			}
			return
		case "set-key":
			if len(args) != 2 {
				fail(tr("set-key 需要一个 provider 别名", "set-key requires a provider alias"))
			}
			if err := runSetKey(cli, args[1]); err != nil {
				fail(err.Error())
			}
			return
		case "remove":
			if len(args) != 2 {
				fail(tr("remove 需要一个 provider 别名", "remove requires a provider alias"))
			}
			if err := runRemove(args[1]); err != nil {
				fail(err.Error())
			}
			return
		}
	}

	var alias, model string
	skip, intl, dryRun := false, false, false
	var passthrough []string
	scopedCandidate := ""
	scopedCandidateIndex := -1
	noMore := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case noMore:
			passthrough = append(passthrough, a)
		case a == "--":
			noMore = true
		case a == "-y" || a == "--yes":
			skip = true
		case a == "--intl":
			intl = true
		case a == "--dry-run":
			dryRun = true
		case a == "-m" || a == "--model":
			if i+1 >= len(args) {
				fail(tr("--model 需要一个参数", "--model requires an argument"))
			}
			model = args[i+1]
			i++
		case strings.HasPrefix(a, "--model="):
			model = strings.TrimPrefix(a, "--model=")
		case strings.HasPrefix(a, "-"):
			passthrough = append(passthrough, a) // 未知 flag 透传
		default:
			if alias == "" {
				alias = a
			} else {
				if scopedCandidate == "" {
					scopedCandidate = a
					scopedCandidateIndex = len(passthrough)
				}
				passthrough = append(passthrough, a)
			}
		}
	}

	if alias == "" {
		printQuickStart(prog, cli)
		return
	}

	if alias == "custom" {
		if dryRun {
			fmt.Fprint(os.Stderr, tr("（custom 需交互输入端点/model/key 后探测，--dry-run 不适用）\n", "custom requires interactive endpoint/model/key input and probing; --dry-run is unavailable\n"))
			return
		}
		if err := runCustom(cli, skip, passthrough); err != nil {
			fail(err.Error())
		}
		return
	}

	if alias == "def" {
		if model != "" {
			fail(tr("def 不接受 --model；需要原生参数时请放在 -- 后", "def does not accept --model; place native CLI arguments after --"))
		}
		if intl {
			fail(tr("def 使用原生账号，不接受 --intl", "def uses the native account and does not accept --intl"))
		}
		if dryRun {
			previewDefault(cli, skip, passthrough)
			return
		}
		if err := launchDefault(cli, skip, passthrough); err != nil {
			if ee, ok := err.(interface{ ExitCode() int }); ok {
				os.Exit(ee.ExitCode())
			}
			fail(err.Error())
		}
		return
	}

	idx := buildIndex()
	r, ok := idx[alias]
	if !ok {
		fail(fmt.Sprintf(tr("未知别名: %s\n运行 `%s list` 查看可用别名", "Unknown alias: %s\nRun `%s list` to see available aliases"), alias, prog))
	}
	if scopedCandidate != "" && r.Prov.Alias == alias {
		if scoped, found := resolveProviderModel(r.Prov, scopedCandidate); found {
			if model != "" {
				fail(tr("模型短名不能和 --model 同时使用", "A model shortcut cannot be used together with --model"))
			}
			r = scoped
			passthrough = append(passthrough[:scopedCandidateIndex], passthrough[scopedCandidateIndex+1:]...)
		} else if knownModelSelector(scopedCandidate) {
			fail(fmt.Sprintf(tr("%s 当前没有 %s", "%s does not currently offer %s"), r.Prov.Name, scopedCandidate))
		}
	}
	if !r.Prov.supports(cli) {
		fail(fmt.Sprintf(tr("%s 不支持 %s（端点协议限制，无代理）。\n支持: %s", "%s does not support %s (endpoint protocol limitation; no proxy).\nSupported: %s"),
			r.Prov.Name, cli, strings.Join(r.Prov.CLI, ", ")))
	}
	if intl && !r.Prov.hasIntlFor(cli) {
		fail(fmt.Sprintf(tr("%s 没有可供 %s 使用的海外端点", "%s has no international endpoint for %s"), r.Prov.Name, cli))
	}

	chosen := r.Model.ID
	if model != "" {
		chosen = model
	}

	if dryRun {
		preview(cli, r.Prov, chosen, skip, intl, passthrough)
		return
	}

	var err error
	switch cli {
	case "claude":
		err = launchClaude(r.Prov, chosen, skip, intl, passthrough)
	case "codex":
		err = launchCodex(r.Prov, chosen, skip, intl, passthrough)
	case "opencode":
		err = launchOpencode(r.Prov, chosen, skip, intl, passthrough)
	}
	if err != nil {
		if ee, ok := err.(interface{ ExitCode() int }); ok {
			os.Exit(ee.ExitCode())
		}
		fail(err.Error())
	}
}

func printQuickStart(prog, cli string) {
	fmt.Printf(tr("%s：用指定 provider / model 启动 %s\n", "%s: launch %s with a selected provider and model\n"), prog, cli)
	fmt.Printf(tr("用法: %s def，%s <模型短名>，或 %s <来源短名> [模型短名]\n", "Usage: %s def, %s <model shortcut>, or %s <source shortcut> [model shortcut]\n"), prog, prog, prog)
	fmt.Printf(tr("示例: %s def    %s k3    %s sf k27\n", "Examples: %s def    %s k3    %s sf k27\n"), prog, prog, prog)
	fmt.Printf(tr("可用别名: %s list    配置: %s config\n", "Aliases: %s list    Configuration: %s config\n"), prog, prog)
	fmt.Printf(tr("完整帮助: %s --help\n", "Full help: %s --help\n"), prog)
}

func isReservedAlias(alias string) bool {
	switch alias {
	case "config", "add", "set-key", "remove", "update", "version", "doctor", "audit-probes", "list", "ls", "help", "custom", "def":
		return true
	default:
		return false
	}
}

func isHelpCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	return args[0] == "-h" || args[0] == "--help" || args[0] == "help"
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "✗ "+msg)
	os.Exit(1)
}

// ---- 对照表渲染（按显示宽度对齐，兼容中日韩双宽字符）----

func printTable() {
	fmt.Println()
	fmt.Println("  " + pad(tr("别名（版本）", "Alias (version)"), 25) + pad("Provider", 29) + pad(tr("默认模型", "Default model"), 36) + pad(tr("入口", "Entry"), 13) + "intl")
	fmt.Println("  " + strings.Repeat("-", 107))
	fmt.Println("  " + pad("def", 25) + pad(tr("原生账号 / 配置", "Native account / config"), 29) + pad(tr("由对应 CLI 决定", "Selected by the CLI"), 36) + pad("cld/cdx/opc", 13) + "—")
	all := append([]Provider{}, catalogProviders()...)
	all = append(all, loadCustomProfiles()...)
	for i := range all {
		p := &all[i]
		var tags []string
		var latest string
		for _, m := range p.Models {
			if m.Tag != "" {
				tags = append(tags, m.Tag)
			}
			if m.Latest {
				latest = m.ID
			}
		}
		intlMark := "—"
		if p.hasIntl() {
			intlMark = "--intl"
		}
		aliasLines := wrapAliasCell(p.Alias, tags, 25)
		fmt.Println("  " + pad(aliasLines[0], 25) + pad(localizedProviderName(p.Name), 29) + pad(latest, 36) + pad(entrySummary(p), 13) + intlMark)
		for _, continuation := range aliasLines[1:] {
			fmt.Println("  " + pad(continuation, 25))
		}
		if shortcuts := modelShortcutExamples(p); len(shortcuts) > 0 {
			fmt.Println("      " + tr("可选: ", "Options: ") + strings.Join(shortcuts, ", "))
		}
	}
}

func modelShortcutExamples(p *Provider) []string {
	var shortcuts []string
	for _, model := range p.Models {
		if model.Short == "" {
			continue
		}
		if model.Source == "official" {
			shortcuts = append(shortcuts, model.Short)
			continue
		}
		shortcuts = append(shortcuts, p.Alias+" "+model.Short)
	}
	return shortcuts
}

func wrapAliasCell(alias string, tags []string, width int) []string {
	if len(tags) == 0 {
		return []string{alias}
	}
	var lines []string
	remaining := tags
	first := true
	for len(remaining) > 0 {
		prefix := strings.Repeat(" ", dispWidth(alias)+1) + "("
		if first {
			prefix = alias + " ("
		}
		line := prefix
		used := 0
		for used < len(remaining) {
			separator := ""
			if used > 0 {
				separator = ","
			}
			candidate := line + separator + remaining[used] + ")"
			if used > 0 && dispWidth(candidate) > width {
				break
			}
			line += separator + remaining[used]
			used++
		}
		lines = append(lines, line+")")
		remaining = remaining[used:]
		first = false
	}
	return lines
}

func entrySummary(p *Provider) string {
	var entries []string
	for _, item := range []struct {
		cli, entry string
	}{{"claude", "cld"}, {"codex", "cdx"}, {"opencode", "opc"}} {
		if p.supports(item.cli) {
			entries = append(entries, item.entry)
		}
	}
	return strings.Join(entries, "/")
}

func pad(s string, width int) string {
	d := dispWidth(s)
	if d < width {
		return s + strings.Repeat(" ", width-d)
	}
	// 超宽内容（如较长的自定义别名）：也至少留一个空格，避免与下一列粘在一起。
	return s + " "
}

func dispWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r >= 0x1100 && (r <= 0x115F ||
			(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) ||
			(r >= 0xAC00 && r <= 0xD7A3) ||
			(r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFE30 && r <= 0xFE4F) ||
			(r >= 0xFF00 && r <= 0xFF60) ||
			(r >= 0xFFE0 && r <= 0xFFE6)):
			w += 2
		default:
			w++
		}
	}
	return w
}
