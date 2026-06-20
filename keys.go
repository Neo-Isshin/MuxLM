package main

import (
	"bufio"
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

// getKey 取某 provider 的 key；找不到则交互式让用户输入（隐藏输入），
// 并询问是否保存到 keys.env 以便下次直接使用。
func getKey(envName, provName string) (string, error) {
	if v := loadKeys()[envName]; v != "" {
		return v, nil
	}

	fmt.Fprintf(os.Stderr, "\n⚠️  未找到 %s（%s 需要）。\n", envName, provName)
	fmt.Fprintf(os.Stderr, "请输入 %s（输入隐藏，直接回车则取消）: ", envName)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("读取输入失败: %w", err)
	}
	val := strings.TrimSpace(string(b))
	if val == "" {
		return "", fmt.Errorf("已取消：未提供 %s", envName)
	}

	if promptYesNo("是否保存到 " + keysFile() + " 以便下次直接使用? [Y/n] ") {
		saveKey(envName, val)
	}
	return val, nil
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

func promptYesNo(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	r := bufio.NewReader(os.Stdin)
	s, _ := r.ReadString('\n')
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "" || s == "y" || s == "yes"
}
