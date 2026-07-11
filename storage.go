package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func configDir() string {
	if d := os.Getenv("CX_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cx")
}

func providersDir() string         { return filepath.Join(configDir(), "providers") }
func providerDir(id string) string { return filepath.Join(providersDir(), safeID(id)) }
func catalogCacheFile() string     { return filepath.Join(configDir(), "catalog.json") }

func safeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "." || out == ".." || strings.HasPrefix(out, ".") {
		return ""
	}
	return out
}

func ensurePrivateDir(dir string) error {
	root, _ := filepath.Abs(configDir())
	cur, _ := filepath.Abs(dir)
	if cur == root || strings.HasPrefix(cur, root+string(os.PathSeparator)) {
		for {
			if fi, err := os.Lstat(cur); err == nil && fi.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("拒绝使用符号链接目录: %s", cur)
			}
			if cur == root {
				break
			}
			cur = filepath.Dir(cur)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// #nosec G302 -- 0700 是目录的预期私有权限；文件使用 0600。
	return os.Chmod(dir, 0o700)
}

func readPrivateFile(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("拒绝读取符号链接: %s", path)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	// #nosec G304 -- path 只由受控的 config root/provider ID/固定文件名组成，且已拒绝 symlink。
	return os.ReadFile(path)
}

func atomicWriteJSON(path string, v any) error {
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("拒绝写入符号链接: %s", path)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.CreateTemp(filepath.Dir(path), ".cx-write-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

type fileSecrets map[string]string

func fileSecretsPath(providerID string) string {
	return filepath.Join(providerDir(providerID), "secrets.json")
}

func secretBackend() string {
	if b := strings.ToLower(os.Getenv("CX_SECRET_BACKEND")); b != "" {
		return b
	}
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("security"); err == nil {
			return "keychain"
		}
	}
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("secret-tool"); err == nil {
			return "secret-service"
		}
	}
	return "file"
}

func secretSet(providerID, ref, value string) (string, error) {
	backend := secretBackend()
	switch backend {
	case "keychain":
		// #nosec G204 -- 可执行文件固定，ref 由程序生成并在读取元数据时严格校验。
		cmd := exec.Command("security", "add-generic-password", "-U", "-a", ref, "-s", "ez-switch", "-w")
		// -w 不带参数可避免 key 进入进程列表；security 会要求输入两次确认。
		cmd.Stdin = strings.NewReader(keychainPasswordInput(value))
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("写入 macOS Keychain 失败: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	case "secret-service":
		// #nosec G204 -- 可执行文件固定，ref 由程序生成并在读取元数据时严格校验。
		cmd := exec.Command("secret-tool", "store", "--label=ez-switch", "service", "ez-switch", "account", ref)
		cmd.Stdin = strings.NewReader(value + "\n")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("写入 Secret Service 失败: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	case "file":
		path := fileSecretsPath(providerID)
		m := fileSecrets{}
		if b, err := readPrivateFile(path); err == nil {
			if err := json.Unmarshal(b, &m); err != nil {
				return "", fmt.Errorf("密钥文件损坏，拒绝覆盖: %w", err)
			}
		}
		m[ref] = value
		if err := atomicWriteJSON(path, m); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("未知密钥后端 %q", backend)
	}
	return backend, nil
}

func keychainPasswordInput(value string) string { return value + "\n" + value + "\n" }

func secretGet(providerID, backend, ref string) (string, error) {
	switch backend {
	case "keychain":
		// #nosec G204 -- 可执行文件固定，ref 来自已校验的本地元数据。
		out, err := exec.Command("security", "find-generic-password", "-a", ref, "-s", "ez-switch", "-w").Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	case "secret-service":
		// #nosec G204 -- 可执行文件固定，ref 来自已校验的本地元数据。
		out, err := exec.Command("secret-tool", "lookup", "service", "ez-switch", "account", ref).Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	case "file":
		b, err := readPrivateFile(fileSecretsPath(providerID))
		if err != nil {
			return "", err
		}
		m := fileSecrets{}
		if err := json.Unmarshal(b, &m); err != nil {
			return "", err
		}
		v := m[ref]
		if v == "" {
			return "", errors.New("密钥不存在")
		}
		return v, nil
	default:
		return "", fmt.Errorf("未知密钥后端 %q", backend)
	}
}

func secretDelete(providerID, backend, ref string) error {
	switch backend {
	case "keychain":
		// #nosec G204 -- 可执行文件固定，ref 来自已校验的本地元数据。
		return exec.Command("security", "delete-generic-password", "-a", ref, "-s", "ez-switch").Run()
	case "secret-service":
		// #nosec G204 -- 可执行文件固定，ref 来自已校验的本地元数据。
		return exec.Command("secret-tool", "clear", "service", "ez-switch", "account", ref).Run()
	case "file":
		path := fileSecretsPath(providerID)
		b, err := readPrivateFile(path)
		if err != nil {
			return err
		}
		m := fileSecrets{}
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		delete(m, ref)
		return atomicWriteJSON(path, m)
	default:
		return fmt.Errorf("未知密钥后端 %q", backend)
	}
}
