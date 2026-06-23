package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const helpText = `ez-switch — 快速切换模型（一个工具，三个入口）

  cdx <别名>  = codex    + <别名>
  cld <别名>  = claude   + <别名>
  opc <别名>  = opencode + <别名>

用法:
  cdx|cld|opc <别名> [-m 模型] [-y] [--intl] [--dry-run] [-- 透传给底层CLI的参数...]

选项:
  -m, --model <id>   覆盖模型 id
  -y, --yes          跳过权限/审批（claude/codex）；opencode 写入宽松权限配置
      --intl         使用海外端点（MiniMax/SiliconFlow 等）
      --dry-run      只打印将执行的命令，不真正启动
  -h, --help         显示此帮助
  list / ls          显示对照表

别名规则:
  · 裸别名（glm/m/ds...）永远 = 该厂商最新模型
  · 版本别名（glm52/m3...）= 裸别名 + 版本号，锁定具体版本
  · -m 可在任意别名上再次覆盖模型

示例:
  cld glm                       # claude + GLM 最新 (glm-5.2)
  cdx glm                       # codex + GLM
  opc m                         # opencode + MiniMax
  cld m -y                      # claude + MiniMax + 跳过权限
  cdx m --intl                  # codex + MiniMax 海外端点
  opc ds -m deepseek-reasoner
`

func main() {
	prog := filepath.Base(os.Args[0])
	args := os.Args[1:]

	if len(args) == 0 || hasAny(args, "-h", "--help", "help") {
		fmt.Print(helpText)
		printTable()
		return
	}
	if args[0] == "list" || args[0] == "ls" {
		printTable()
		return
	}

	// argv[0] 决定目标 CLI：cdx→codex, cld→claude, opc→opencode
	cli := ""
	switch prog {
	case "cdx":
		cli = "codex"
	case "cld":
		cli = "claude"
	case "opc":
		cli = "opencode"
	default:
		// 以 ez-switch（或其它名字）直接运行：只显示帮助/对照表，不启动
		fmt.Print(helpText)
		printTable()
		return
	}

	var alias, model string
	skip, intl, dryRun := false, false, false
	var passthrough []string
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
				fail("--model 需要一个参数")
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
				passthrough = append(passthrough, a)
			}
		}
	}

	if alias == "" {
		fmt.Print(helpText)
		printTable()
		return
	}

	if alias == "custom" {
		if dryRun {
			fmt.Fprint(os.Stderr, "（custom 需交互输入端点/model/key 后探测，--dry-run 不适用）\n")
			return
		}
		if err := runCustom(cli, skip, passthrough); err != nil {
			fail(err.Error())
		}
		return
	}

	idx := buildIndex()
	r, ok := idx[alias]
	if !ok {
		fail("未知别名: " + alias + "\n运行 `cld list` 查看对照表")
	}
	if !r.Prov.supports(cli) {
		fail(fmt.Sprintf("%s 不支持 %s（端点协议限制，无代理）。\n支持: %s",
			r.Prov.Name, cli, strings.Join(r.Prov.CLI, ", ")))
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
		fail(err.Error())
	}
}

func hasAny(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "✗ "+msg)
	os.Exit(1)
}

// ---- 对照表渲染（按显示宽度对齐，兼容中日韩双宽字符）----

func printTable() {
	fmt.Println()
	fmt.Println("  " +
		pad("别名", 8) + pad("版本别名", 26) + pad("厂商", 30) + pad("默认模型", 36) +
		pad("claude", 7) + pad("codex", 7) + pad("opencode", 9) + "intl")
	fmt.Println("  " + strings.Repeat("-", 126))
	all := append([]Provider{}, providers...)
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
		tagStr := strings.Join(tags, " · ")
		if tagStr == "" {
			tagStr = "-"
		}
		intlMark := "—"
		if p.hasIntl() {
			intlMark = "--intl"
		}
		fmt.Println("  " +
			pad(p.Alias, 8) + pad(tagStr, 26) + pad(p.Name, 30) + pad(latest, 36) +
			pad(yes(p.supports("claude")), 7) + pad(yes(p.supports("codex")), 7) +
			pad(yes(p.supports("opencode")), 9) + intlMark)
	}
}

func yes(b bool) string {
	if b {
		return "✅"
	}
	return "—"
}

func pad(s string, width int) string {
	if d := dispWidth(s); d < width {
		return s + strings.Repeat(" ", width-d)
	}
	return s
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
