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

const maxPrivateFileBytes = 2 << 20

func configDir() string {
	roots, err := configRootsForReadE()
	if err == nil {
		return roots[0]
	}
	return unavailableConfigRoot()
}

func unavailableConfigRoot() string {
	// Keep path-only callers absolute even when HOME is unavailable. All reads
	// and writes resolve the error through configRootsForReadE and stop before
	// touching this sentinel. Nesting beneath the null device also makes direct
	// Lstat/remove callers fail closed instead of addressing a real directory.
	if nullDevice, absErr := filepath.Abs(os.DevNull); absErr == nil {
		return filepath.Join(nullDevice, ".providerdeck-home-unavailable")
	}
	return filepath.Join(string(os.PathSeparator), ".providerdeck-home-unavailable")
}

// configRootsForRead keeps the rename non-destructive. New paths win, while a
// missing file/provider may still be read from the legacy cx tree. Explicit
// config overrides are intentionally isolated and never fall back elsewhere.
func configRootsForRead() []string {
	roots, err := configRootsForReadE()
	if err != nil {
		return []string{unavailableConfigRoot()}
	}
	return roots
}

func configRootsForReadE() ([]string, error) {
	if d := firstEnv("PROVIDERDECK_CONFIG_DIR", "CX_CONFIG_DIR"); d != "" {
		abs, err := filepath.Abs(d)
		if err != nil {
			return nil, fmt.Errorf("配置目录无效: %w", err)
		}
		return []string{abs}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = errors.New("HOME 为空")
		}
		return nil, fmt.Errorf("无法确定配置目录: %w；请设置 HOME 或 PROVIDERDECK_CONFIG_DIR", err)
	}
	if !filepath.IsAbs(home) {
		return nil, fmt.Errorf("HOME 必须是绝对路径；请设置 HOME 或 PROVIDERDECK_CONFIG_DIR")
	}
	home = filepath.Clean(home)
	current := filepath.Join(home, ".config", "providerdeck")
	legacy := filepath.Join(home, ".config", "cx")
	if _, err := os.Lstat(current); err == nil || !os.IsNotExist(err) {
		roots := []string{current}
		if _, legacyErr := os.Lstat(legacy); legacyErr == nil || !os.IsNotExist(legacyErr) {
			roots = append(roots, legacy)
		}
		return roots, nil
	}
	if _, err := os.Lstat(legacy); err == nil || !os.IsNotExist(err) {
		return []string{legacy}, nil
	}
	return []string{current}, nil
}

func providersDir() string         { return filepath.Join(configDir(), "providers") }
func providerDir(id string) string { return filepath.Join(providersDir(), safeID(id)) }
func catalogCacheFile() string     { return filepath.Join(configDir(), "catalog.json") }
func updateStateFile() string      { return filepath.Join(configDir(), "update-state.json") }
func updateLockFile() string       { return filepath.Join(configDir(), "update.lock") }
func updateCheckFile() string      { return filepath.Join(configDir(), "update-check.json") }
func releaseCheckFile() string     { return filepath.Join(configDir(), "release-check.json") }

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

func providerDirsForRead() []string {
	roots := configRootsForRead()
	dirs := make([]string, 0, len(roots))
	for _, root := range roots {
		dirs = append(dirs, filepath.Join(root, "providers"))
	}
	return dirs
}

func ensurePrivateDir(dir string) error {
	roots, err := configRootsForReadE()
	if err != nil {
		return err
	}
	return ensurePrivateDirWithin(dir, privateRootForPathWithRoots(dir, roots))
}

func ensurePrivateDirWithin(dir, root string) error {
	root, _ = filepath.Abs(root)
	cur, _ := filepath.Abs(dir)
	if cur != root && !strings.HasPrefix(cur, root+string(os.PathSeparator)) {
		return fmt.Errorf("路径不在配置目录内: %s", cur)
	}
	for check := cur; ; check = filepath.Dir(check) {
		if fi, err := os.Lstat(check); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("拒绝使用符号链接目录: %s", check)
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		if check == root {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// #nosec G302 -- 0700 是目录的预期私有权限；文件使用 0600。
	return os.Chmod(dir, 0o700)
}

func readPrivateFile(path string) ([]byte, error) {
	roots, err := configRootsForReadE()
	if err != nil {
		return nil, err
	}
	root := privateRootForPathWithRoots(path, roots)
	b, err := readPrivateFileWithin(path, root)
	if !os.IsNotExist(err) {
		return b, err
	}
	fallback, fallbackRoot, ok := legacyFallbackPathWithRoots(path, root, roots)
	if !ok {
		return nil, err
	}
	return readPrivateFileWithin(fallback, fallbackRoot)
}

func readPrivateFileWithin(path, root string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("拒绝读取符号链接: %s", path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("拒绝读取非普通文件: %s", path)
	}
	if fi.Size() > maxPrivateFileBytes {
		return nil, fmt.Errorf("文件超过 2 MiB 限制: %s", path)
	}
	if err := ensurePrivateDirWithin(filepath.Dir(path), root); err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	// #nosec G304 -- path 只由受控的 config root/provider ID/固定文件名组成，且已拒绝 symlink。
	return os.ReadFile(path)
}

func privateRootForPathWithRoots(path string, roots []string) string {
	abs, _ := filepath.Abs(path)
	for _, root := range roots {
		rootAbs, _ := filepath.Abs(root)
		if abs == rootAbs || strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
			return root
		}
	}
	return roots[0]
}

func legacyFallbackPathWithRoots(path, root string, roots []string) (string, string, bool) {
	if len(roots) < 2 {
		return "", "", false
	}
	primaryAbs, _ := filepath.Abs(roots[0])
	rootAbs, _ := filepath.Abs(root)
	if rootAbs != primaryAbs {
		return "", "", false
	}
	pathAbs, _ := filepath.Abs(path)
	rel, err := filepath.Rel(primaryAbs, pathAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", false
	}
	return filepath.Join(roots[1], rel), roots[1], true
}

func atomicWriteJSON(path string, v any) error {
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("拒绝写入符号链接: %s", path)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("拒绝替换非普通文件: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if len(b) > maxPrivateFileBytes {
		return fmt.Errorf("写入内容超过 2 MiB 限制: %s", path)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".providerdeck-write-*")
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
	if b := strings.ToLower(firstEnv("PROVIDERDECK_SECRET_BACKEND", "CX_SECRET_BACKEND")); b != "" {
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
		cmd := exec.Command("security", "add-generic-password", "-U", "-a", ref, "-s", secretService, "-w")
		// -w 不带参数可避免 key 进入进程列表；security 会要求输入两次确认。
		cmd.Stdin = strings.NewReader(keychainPasswordInput(value))
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("写入 macOS Keychain 失败: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	case "secret-service":
		// #nosec G204 -- 可执行文件固定，ref 由程序生成并在读取元数据时严格校验。
		cmd := exec.Command("secret-tool", "store", "--label="+appName, "service", secretService, "account", ref)
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
		} else if !os.IsNotExist(err) {
			return "", err
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
		for _, service := range []string{secretService, legacySecretService} {
			// #nosec G204 -- 可执行文件固定，ref 来自已校验的本地元数据。
			out, err := exec.Command("security", "find-generic-password", "-a", ref, "-s", service, "-w").Output()
			if err == nil {
				return strings.TrimSpace(string(out)), nil
			}
		}
		return "", errors.New("密钥不存在")
	case "secret-service":
		for _, service := range []string{secretService, legacySecretService} {
			// #nosec G204 -- 可执行文件固定，ref 来自已校验的本地元数据。
			out, err := exec.Command("secret-tool", "lookup", "service", service, "account", ref).Output()
			if err == nil && strings.TrimSpace(string(out)) != "" {
				return strings.TrimSpace(string(out)), nil
			}
		}
		return "", errors.New("密钥不存在")
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
		return deleteSecretFromServices(func(service string) error {
			// #nosec G204 -- 可执行文件固定，ref 来自已校验的本地元数据。
			return exec.Command("security", "delete-generic-password", "-a", ref, "-s", service).Run()
		})
	case "secret-service":
		return deleteSecretFromServices(func(service string) error {
			// #nosec G204 -- 可执行文件固定，ref 来自已校验的本地元数据。
			return exec.Command("secret-tool", "clear", "service", service, "account", ref).Run()
		})
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

func deleteSecretFromServices(remove func(service string) error) error {
	var lastErr error
	deleted := false
	for _, service := range []string{secretService, legacySecretService} {
		if err := remove(service); err == nil {
			deleted = true
		} else {
			lastErr = err
		}
	}
	if deleted {
		return nil
	}
	return lastErr
}
