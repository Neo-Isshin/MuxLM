package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

// keysFile 是本地保存 key 的文件（可选；权限 600）。
func keysFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cx", "keys.env")
}

// loadKeys 合并 环境变量 + keys.env 文件（环境变量优先）。
func loadKeys() map[string]string {
	k := make(map[string]string)
	for _, kv := range os.Environ() {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			k[kv[:eq]] = kv[eq+1:]
		}
	}
	if data, err := os.ReadFile(keysFile()); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if eq := strings.IndexByte(line, '='); eq > 0 {
				key := strings.TrimSpace(line[:eq])
				val := strings.TrimSpace(line[eq+1:])
				if _, ok := k[key]; !ok {
					k[key] = val
				}
			}
		}
	}
	return k
}

// getKey 取某 provider 的 key。
//   - 海外端点（--intl，或仅有海外 key）用 <KeyEnv>_INTL；国内用 KeyEnv。
//   - 国内/海外都没 key 时，若 provider 有海外端点，先让用户选端点（据此设置 *intl），
//     再隐藏输入对应 key；否则直接输入国内 key。
//   - 可选保存到 keys.env（按区域用对应 env 名）。
func getKey(p *Provider, intl *bool) (string, error) {
	if p.Key != "" {
		return p.Key, nil // custom 自定义别名：内联 key，跳过 env/交互
	}
	keys := loadKeys()
	cnEnv := p.KeyEnv
	intlEnv := ""
	if p.hasIntl() {
		intlEnv = p.KeyEnv + "_INTL"
	}

	// provider 无海外端点时，--intl 无意义，归一到国内
	if *intl && intlEnv == "" {
		*intl = false
	}

	// 显式选海外
	if *intl {
		if v := keys[intlEnv]; v != "" {
			return v, nil
		}
		return promptKey(p, intlEnv, true)
	}

	// 未显式指定：优先国内 key
	if v := keys[cnEnv]; v != "" {
		return v, nil
	}
	// 只有海外 key → 自动切海外
	if intlEnv != "" {
		if v := keys[intlEnv]; v != "" {
			*intl = true
			return v, nil
		}
		// 都没有 → 选端点
		fmt.Fprintf(os.Stderr, "\n⚠️  未找到 %s。%s 区分 国内/海外（两套独立账号，key 不同）。\n", cnEnv, p.Name)
		if chooseIntl(p) {
			*intl = true
			return promptKey(p, intlEnv, true)
		}
	}
	return promptKey(p, cnEnv, false)
}

// promptKey 隐藏输入 key，并询问是否保存（按区域用对应 env 名）。
func promptKey(p *Provider, envName string, intl bool) (string, error) {
	region := "国内"
	host := p.host(false)
	if intl {
		region = "海外"
		host = p.host(true)
	}
	fmt.Fprintf(os.Stderr, "请输入 %s（%s %s，输入隐藏，回车取消）: ", envName, region, host)
	val, err := readHidden()
	if err != nil {
		return "", fmt.Errorf("读取输入失败: %w", err)
	}
	if val == "" {
		return "", fmt.Errorf("已取消：未提供 %s", envName)
	}
	if promptYesNo("是否保存到 " + keysFile() + " 以便下次免输? [Y/n] ") {
		saveKey(envName, val)
	}
	return val, nil
}

// chooseIntl 让用户在国内/海外端点间选择，默认国内。
func chooseIntl(p *Provider) bool {
	fmt.Fprintf(os.Stderr, "选择端点（输入不回显）：\n  1) 国内  %s   （默认，回车）\n  2) 海外  %s\n", p.host(false), p.host(true))
	fmt.Fprint(os.Stderr, "请选择 [1]: ")
	s, err := readHidden()
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return false
	}
	if s == "2" {
		fmt.Fprintf(os.Stderr, "→ 已选海外 %s\n", p.host(true))
		return true
	}
	fmt.Fprintf(os.Stderr, "→ 已选国内 %s\n", p.host(false))
	return false
}

// readHidden 隐藏回显地读一行。
//   - TTY：用 term.ReadPassword 关回显（交互场景，key 不被看见）。
//   - 非 TTY（管道/重定向）：回显本就无法控制，退化为读一行（保证脚本/管道可喂入）。
//
// 所有交互读取统一走这里，避免 bufio 缓冲与 term 直读混用导致管道输入错位。
func readHidden() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return readLineCooked(), nil
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func saveKey(envName, val string) {
	f := keysFile()
	_ = os.MkdirAll(filepath.Dir(f), 0o700)
	content, _ := os.ReadFile(f)
	s := string(content)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	s += envName + "=" + val + "\n"
	if err := os.WriteFile(f, []byte(s), 0o600); err == nil {
		fmt.Fprintf(os.Stderr, "✅ 已保存（权限 600）\n")
	} else {
		fmt.Fprintf(os.Stderr, "⚠️  保存失败: %v\n", err)
	}
}

// promptYesNo 询问 Y/n，默认 Y（回车=Y）。输入不回显。
func promptYesNo(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	s, err := readHidden()
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return false
	}
	s = strings.ToLower(s)
	return s == "" || s == "y" || s == "yes"
}
