package main

import (
	"fmt"
	"io"
	"strings"
)

// runAuditProbes 走遍 catalog 中所有 (provider × cli × region) 组合，
// 用一个明显无效的假 key 发出与 save-key 时同款的探测请求，按 HTTP 响应分类。
//
// 目的不是验证 key 真假——而是确认 catalog 里的每条 URL/protocol 路径对「假 key」
// 能给出 401/403。其它响应（404/400/429/5xx/连不上）说明协议路径或 key 处理方式
// 与 muxlm 的预期不一致，存 key 时会被新的 checkKey 拦下来要求 explicit "yes"。
//
// 用法:
//
//	cld audit-probes                  # 全量审计
//	cld audit-probes -p m             # 只看 m 的所有 CLI/region
//	cld audit-probes --alias m        # 同上
func runAuditProbes(args []string, stdout io.Writer) error {
	var only string
	rest := args
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch a {
		case "-p", "--provider", "--alias":
			if i+1 >= len(rest) {
				return fmt.Errorf(tr("%s 需要一个 provider 别名", "%s requires a provider alias"), a)
			}
			only = rest[i+1]
			rest = append(rest[:i], rest[i+1:]...)
			i--
		case "-h", "--help":
			fmt.Fprintln(stdout, tr("用法: audit-probes [-p <provider 别名>]", "Usage: audit-probes [-p <provider alias>]"))
			return nil
		default:
			return fmt.Errorf(tr("audit-probes 不接受额外参数: %s", "audit-probes does not accept argument: %s"), a)
		}
	}

	type row struct {
		alias, plan, cli, region, protocol, model, url string
		reachable                                      bool
		code                                           int
	}

	provs := catalogProviders()
	var all, susp []row

	for i := range provs {
		p := &provs[i]
		if only != "" && p.Alias != only && p.providerID() != only {
			continue
		}
		model := pickAuditModel(p)
		if model == "" {
			continue
		}
		for _, cli := range p.CLI {
			regions := []string{"cn"}
			if p.hasIntlFor(cli) {
				regions = append(regions, "intl")
			}
			for _, region := range regions {
				intl := region == "intl"
				proto, base := keyProbeTarget(p, cli, intl)
				if base == "" {
					continue
				}
				url := strings.TrimRight(base, "/")
				switch proto {
				case "anthropic":
					url += "/v1/messages"
				case "responses":
					url += "/responses"
				default:
					url += "/chat/completions"
				}
				reachable, code, _ := probe(proto, base, model, "test-fake-key-audit-only")
				r := row{
					alias:     p.Alias,
					plan:      p.planID(),
					cli:       cli,
					region:    region,
					protocol:  proto,
					model:     model,
					url:       url,
					reachable: reachable,
					code:      code,
				}
				all = append(all, r)
				if isSuspiciousProbe(reachable, code) {
					susp = append(susp, r)
				}
			}
		}
	}

	fmt.Fprintln(stdout, strings.Repeat("=", 100))
	fmt.Fprintln(stdout, tr("可疑结果（HTTP 不是 401/403/2xx —— 意味着端点不认识这个协议路径或 key 处理方式不同）", "Suspicious results (HTTP is not 401/403/2xx — the endpoint may not recognize this protocol path or handles keys differently)"))
	fmt.Fprintln(stdout, strings.Repeat("=", 100))
	if len(susp) == 0 {
		fmt.Fprintln(stdout, tr("  (无)", "  (none)"))
	} else {
		for _, r := range susp {
			fmt.Fprintf(stdout, "  %-10s %-8s %-8s %-5s %-9s -> %s\n",
				r.alias, r.plan, r.cli, r.region, r.protocol, r.url)
			fmt.Fprintf(stdout, "           model=%q  reachable=%v  HTTP=%d\n", r.model, r.reachable, r.code)
			fmt.Fprintln(stdout)
		}
	}

	fmt.Fprintln(stdout, strings.Repeat("=", 100))
	fmt.Fprintln(stdout, tr("全部结果", "All results"))
	fmt.Fprintln(stdout, strings.Repeat("=", 100))
	prev := ""
	for _, r := range all {
		sig := r.alias + "/" + r.plan + "/" + r.cli + "/" + r.region
		if sig != prev {
			fmt.Fprintln(stdout)
			prev = sig
		}
		fmt.Fprintf(stdout, "  %-10s %-8s %-8s %-5s %-9s HTTP=%-4d  %s\n",
			r.alias, r.plan, r.cli, r.region, r.protocol, r.code, r.url)
	}

	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, tr("汇总: %d 条可疑 / %d 条总计\n", "Summary: %d suspicious / %d total\n"), len(susp), len(all))
	return nil
}

// pickAuditModel 选 audit 要 probe 的 model：latest → short → 第一个。
// 与 probe() 的契约：必须有 model id 字段，否则所有响应都是 400 误判。
func pickAuditModel(p *Provider) string {
	if len(p.Models) == 0 {
		return ""
	}
	for _, m := range p.Models {
		if m.Latest {
			return m.ID
		}
	}
	for _, m := range p.Models {
		if m.Short != "" {
			return m.ID
		}
	}
	return p.Models[0].ID
}

// isSuspiciousProbe 判定探测结果是否需要 checkKey 的 explicit-yes 确认。
// 与 keys.go checkKey 的判定完全一致：只有 2xx 算"通过"，401/403 算"key 错"，
// 其余一律 ambiguous。
func isSuspiciousProbe(reachable bool, code int) bool {
	if !reachable {
		return true
	}
	if code >= 200 && code < 300 {
		return false
	}
	if code == 401 || code == 403 {
		return false
	}
	return true
}
