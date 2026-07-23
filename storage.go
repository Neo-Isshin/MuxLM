package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
		return filepath.Join(nullDevice, ".muxlm-home-unavailable")
	}
	return filepath.Join(string(os.PathSeparator), ".muxlm-home-unavailable")
}

// configRootsForRead keeps the rename non-destructive. New paths win, while a
// missing file/provider may still be read from the ProviderDeck and cx trees.
// Explicit config overrides are intentionally isolated and never fall back.
func configRootsForRead() []string {
	roots, err := configRootsForReadE()
	if err != nil {
		return []string{unavailableConfigRoot()}
	}
	return roots
}

func configRootsForReadE() ([]string, error) {
	if d := firstEnv("MUXLM_CONFIG_DIR", "PROVIDERDECK_CONFIG_DIR", "CX_CONFIG_DIR"); d != "" {
		abs, err := filepath.Abs(d)
		if err != nil {
			return nil, fmt.Errorf("配置目录无效: %w", err)
		}
		return []string{abs}, nil
	}
	return defaultConfigRootsForReadE(runtime.GOOS)
}

func defaultConfigRootsForReadE(goos string) ([]string, error) {
	// XDG_CONFIG_HOME applies only on Linux. In particular, keep the existing
	// macOS path and legacy-root selection unchanged.
	if goos == "linux" {
		if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" && filepath.IsAbs(xdg) {
			xdg = filepath.Clean(xdg)
			// An explicit XDG root is always the write target. Existing
			// MuxLM/ProviderDeck/cx data in the historical ~/.config tree
			// remains a read-only fallback until rewritten by a normal mutation.
			primary := filepath.Join(xdg, "muxlm")
			var fallbacks []string
			if home, err := validUserHome(); err == nil {
				legacyBase := filepath.Join(home, ".config")
				fallbacks = append(fallbacks,
					filepath.Join(legacyBase, "muxlm"),
					filepath.Join(legacyBase, "providerdeck"),
					filepath.Join(legacyBase, "cx"),
				)
			}
			return appendExistingUnique([]string{primary}, fallbacks), nil
		}
	}
	home, err := validUserHome()
	if err != nil {
		return nil, err
	}
	return selectExistingConfigRoots([]string{
		filepath.Join(home, ".config", "muxlm"),
		filepath.Join(home, ".config", "providerdeck"),
		filepath.Join(home, ".config", "cx"),
	}), nil
}

func validUserHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = errors.New("HOME 为空")
		}
		return "", fmt.Errorf("无法确定配置目录: %w；请设置 HOME 或 MUXLM_CONFIG_DIR", err)
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("HOME 必须是绝对路径；请设置 HOME 或 MUXLM_CONFIG_DIR")
	}
	return filepath.Clean(home), nil
}

func selectExistingConfigRoots(candidates []string) []string {
	primary := -1
	for i, candidate := range candidates {
		if _, err := os.Lstat(candidate); err == nil || !os.IsNotExist(err) {
			primary = i
			break
		}
	}
	if primary == -1 {
		return []string{candidates[0]}
	}
	roots := []string{candidates[primary]}
	for _, candidate := range candidates[primary+1:] {
		if _, err := os.Lstat(candidate); err == nil || !os.IsNotExist(err) {
			roots = append(roots, candidate)
		}
	}
	return roots
}

func appendExistingUnique(roots, candidates []string) []string {
	seen := make(map[string]bool, len(roots)+len(candidates))
	for _, root := range roots {
		seen[filepath.Clean(root)] = true
	}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		if _, err := os.Lstat(candidate); err == nil || !os.IsNotExist(err) {
			roots = append(roots, candidate)
			seen[candidate] = true
		}
	}
	return roots
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
	return readPrivateFileWithRoots(path, roots)
}

func readPrivateFileWithRoots(path string, roots []string) ([]byte, error) {
	if len(roots) == 0 {
		return nil, errors.New("配置目录列表为空")
	}
	root := privateRootForPathWithRoots(path, roots)
	b, err := readPrivateFileWithin(path, root)
	if !os.IsNotExist(err) {
		return b, err
	}
	primaryAbs, _ := filepath.Abs(roots[0])
	rootAbs, _ := filepath.Abs(root)
	pathAbs, _ := filepath.Abs(path)
	rel, relErr := filepath.Rel(primaryAbs, pathAbs)
	if rootAbs != primaryAbs || relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, err
	}
	lastErr := err
	for _, fallbackRoot := range roots[1:] {
		b, fallbackErr := readPrivateFileWithin(filepath.Join(fallbackRoot, rel), fallbackRoot)
		if !os.IsNotExist(fallbackErr) {
			return b, fallbackErr
		}
		lastErr = fallbackErr
	}
	return nil, lastErr
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
	f, err := os.CreateTemp(filepath.Dir(path), ".muxlm-write-*")
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

type secretBackendChoice struct {
	name     string
	explicit bool
	reason   string
}

func secretBackend() string {
	// Keep diagnostics passive: secretBackend is used by doctor and must never
	// contact or unlock a keyring merely to report the selected backend.
	return chooseSecretBackendPassive().name
}

func chooseSecretBackendPassive() secretBackendChoice {
	if b := strings.ToLower(firstEnv("MUXLM_SECRET_BACKEND", "PROVIDERDECK_SECRET_BACKEND", "CX_SECRET_BACKEND")); b != "" {
		return secretBackendChoice{name: b, explicit: true}
	}
	return detectSecretBackend(runtime.GOOS, exec.LookPath, os.Getenv, nil)
}

func chooseSecretBackend() secretBackendChoice {
	if b := strings.ToLower(firstEnv("MUXLM_SECRET_BACKEND", "PROVIDERDECK_SECRET_BACKEND", "CX_SECRET_BACKEND")); b != "" {
		return secretBackendChoice{name: b, explicit: true}
	}
	return detectSecretBackend(runtime.GOOS, exec.LookPath, os.Getenv, probeSecretService)
}

func detectSecretBackend(
	goos string,
	lookPath func(string) (string, error),
	getenv func(string) string,
	probe func() (bool, string),
) secretBackendChoice {
	if goos == "darwin" {
		if _, err := lookPath("security"); err == nil {
			return secretBackendChoice{name: "keychain"}
		}
	}
	if goos == "linux" {
		if _, err := lookPath("secret-tool"); err != nil {
			return secretBackendChoice{name: "file", reason: "未找到 secret-tool"}
		}
		if !visibleSessionBus(getenv) {
			return secretBackendChoice{name: "file", reason: "当前会话没有可用的 D-Bus"}
		}
		if probe == nil {
			return secretBackendChoice{name: "secret-service"}
		}
		if ok, reason := probe(); ok {
			return secretBackendChoice{name: "secret-service"}
		} else {
			return secretBackendChoice{name: "file", reason: reason}
		}
	}
	return secretBackendChoice{name: "file"}
}

func visibleSessionBus(getenv func(string) string) bool {
	if strings.TrimSpace(getenv("DBUS_SESSION_BUS_ADDRESS")) != "" {
		return true
	}
	runtimeDir := strings.TrimSpace(getenv("XDG_RUNTIME_DIR"))
	if runtimeDir == "" || !filepath.IsAbs(runtimeDir) {
		return false
	}
	info, err := os.Lstat(filepath.Join(filepath.Clean(runtimeDir), "bus"))
	return err == nil && info.Mode()&os.ModeSymlink == 0
}

func probeSecretService() (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// A lookup for an intentionally unused pair tests the D-Bus service without
	// modifying the keyring. secret-tool exits 1 with no stderr when no matching
	// password exists; that still proves the service was reached.
	cmd := exec.CommandContext(ctx, "secret-tool", "lookup",
		"service", "muxlm-connectivity-probe-v1",
		"account", "no-password-is-stored-for-this-marker")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, ""
	}
	if ctx.Err() != nil {
		return false, "Secret Service 响应超时"
	}
	detail := strings.TrimSpace(stderr.String())
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && detail == "" {
		return true, ""
	}
	if detail != "" {
		if len(detail) > 160 {
			detail = detail[:160] + "…"
		}
		return false, "Secret Service 不可用：" + detail
	}
	return false, "Secret Service 不可用"
}

func secretSet(providerID, ref, value string) (string, error) {
	return secretSetWithChoice(providerID, ref, value, chooseSecretBackend(), runtime.GOOS)
}

func secretSetWithChoice(providerID, ref, value string, choice secretBackendChoice, goos string) (string, error) {
	if goos == "linux" && choice.name == "file" && !choice.explicit {
		reason := strings.TrimSpace(choice.reason)
		if reason == "" {
			reason = "系统密钥库不可用"
		}
		return "", fmt.Errorf("%s；为避免自动降低密钥安全性，未写入明文文件。确认后请设置 MUXLM_SECRET_BACKEND=file 再试", reason)
	}
	return secretSetWithBackend(providerID, ref, value, choice.name)
}

func secretSetWithBackend(providerID, ref, value, backend string) (string, error) {
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
			return "", fmt.Errorf("写入 Secret Service 失败: %v (%s)；如确认接受 0600 权限的明文文件，可设置 MUXLM_SECRET_BACKEND=file 后重试", err, strings.TrimSpace(string(out)))
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
		for _, service := range secretServicesForRead() {
			// #nosec G204 -- 可执行文件固定，ref 来自已校验的本地元数据。
			out, err := exec.Command("security", "find-generic-password", "-a", ref, "-s", service, "-w").Output()
			if err == nil {
				return strings.TrimSpace(string(out)), nil
			}
		}
		return "", errors.New("密钥不存在")
	case "secret-service":
		for _, service := range secretServicesForRead() {
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
	for _, service := range secretServicesForRead() {
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
