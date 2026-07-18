package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxDoctorMetadataBytes = maxPrivateFileBytes

type doctorCatalogStatus struct {
	revision string
	origin   string
	cache    string
	warnings []string
	errors   []string
}

type doctorConfigStatus struct {
	detail   string
	warnings []string
	errors   []string
}

// runDoctor performs local, read-only diagnostics. In particular, it never
// runs the update client and never resolves a secret reference.
func runDoctor(w io.Writer) error {
	catalog := inspectDoctorCatalog()
	config := inspectDoctorConfig()
	warnings := append([]string{}, catalog.warnings...)
	warnings = append(warnings, config.warnings...)
	problems := append([]string{}, catalog.errors...)
	problems = append(problems, config.errors...)

	fmt.Fprintf(w, "%s %s\n", appName, appVersion)
	fmt.Fprintf(w, "catalog   %s (%s)\n", catalog.revision, catalog.origin)
	if catalog.cache != "" {
		fmt.Fprintf(w, "cache     %q\n", catalog.cache)
	}
	fmt.Fprintf(w, "source    %q\n", catalogURL())
	fmt.Fprintf(w, "config    %q (%s)\n", configDir(), config.detail)
	backend := secretBackend()
	fmt.Fprintf(w, "secrets   %s\n", backend)
	if warning := doctorBackendWarning(backend); warning != "" {
		warnings = append(warnings, warning)
	}

	cliWarnings := 0
	for _, name := range []string{"codex", "claude", "opencode"} {
		path, err := exec.LookPath(name)
		if err != nil {
			fmt.Fprintf(w, "%-9s ⚠ not found\n", name)
			cliWarnings++
			continue
		}
		fmt.Fprintf(w, "%-9s ✓ %q\n", name, path)
	}

	for _, warning := range warnings {
		fmt.Fprintf(w, "warning   ⚠ %s\n", warning)
	}
	for _, problem := range problems {
		fmt.Fprintf(w, "error     ✗ %s\n", problem)
	}
	warningCount := len(warnings) + cliWarnings
	if len(problems) > 0 {
		fmt.Fprintf(w, "status    ✗ %d error(s), %d warning(s)\n", len(problems), warningCount)
		return fmt.Errorf("doctor 检测到 %d 个 catalog/配置错误", len(problems))
	}
	fmt.Fprintf(w, "status    ✓ OK (%d warning(s))\n", warningCount)
	return nil
}

func inspectDoctorCatalog() doctorCatalogStatus {
	status := doctorCatalogStatus{revision: embeddedCatalog.Revision, origin: "embedded"}
	if err := validateCatalog(&embeddedCatalog); err != nil {
		status.errors = append(status.errors, "内置 catalog 无效: "+err.Error())
		return status
	}
	if _, err := validateUpdateURL(catalogURL()); err != nil {
		status.errors = append(status.errors, "catalog source 无效: "+err.Error())
	}

	path, root, rootIndex, found, err := resolveDoctorFile("catalog.json")
	if err != nil {
		status.errors = append(status.errors, "无法检查 catalog cache: "+err.Error())
		return status
	}
	if !found {
		return status
	}
	status.cache = path
	if err := validateDoctorDirWithin(filepath.Dir(path), root); err != nil {
		status.errors = append(status.errors, "catalog cache 路径不安全: "+err.Error())
		return status
	}
	info, err := os.Lstat(path)
	if err != nil {
		status.errors = append(status.errors, "无法检查 catalog cache: "+err.Error())
		return status
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		status.errors = append(status.errors, "catalog cache 不是安全的普通文件")
		return status
	}
	if info.Size() > maxCatalogBytes {
		status.errors = append(status.errors, "catalog cache 超过 2 MiB 限制")
		return status
	}
	data, err := os.ReadFile(path)
	if err != nil {
		status.errors = append(status.errors, "无法读取 catalog cache: "+err.Error())
		return status
	}
	cached, err := decodeCatalog(data)
	if err != nil {
		status.errors = append(status.errors, "catalog cache 损坏: "+err.Error())
		return status
	}
	if compareCatalogRevision(cached.Revision, embeddedCatalog.Revision) < 0 {
		status.warnings = append(status.warnings, fmt.Sprintf("catalog cache %s 旧于内置版本 %s，已忽略", cached.Revision, embeddedCatalog.Revision))
		return status
	}
	if err := validateCachedCatalog(cached); err != nil {
		status.errors = append(status.errors, err.Error())
		return status
	}
	status.revision = cached.Revision
	status.origin = "cache"
	if isDoctorLegacyRoot(root, rootIndex) {
		status.origin = "legacy cache"
	}
	return status
}

func inspectDoctorConfig() doctorConfigStatus {
	status := doctorConfigStatus{detail: "not created"}
	roots, rootsErr := configRootsForReadE()
	if rootsErr != nil {
		status.detail = "unavailable"
		status.errors = append(status.errors, rootsErr.Error())
		return status
	}
	root := roots[0]
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return status
	}
	if err != nil {
		status.detail = "unreadable"
		status.errors = append(status.errors, "无法检查配置目录: "+err.Error())
		return status
	}
	status.detail = fmt.Sprintf("mode %04o", info.Mode().Perm())
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		status.errors = append(status.errors, "配置路径不是安全的普通目录")
		return status
	}
	if info.Mode().Perm()&0o077 != 0 {
		status.warnings = append(status.warnings, fmt.Sprintf("配置目录权限 %04o 偏宽（建议 0700）", info.Mode().Perm()))
	}
	for _, legacyRoot := range roots[1:] {
		legacyInfo, legacyErr := os.Lstat(legacyRoot)
		if os.IsNotExist(legacyErr) {
			continue
		}
		if legacyErr != nil {
			status.errors = append(status.errors, "无法检查 legacy 配置目录: "+legacyErr.Error())
			continue
		}
		if legacyInfo.Mode()&os.ModeSymlink != 0 || !legacyInfo.IsDir() {
			status.errors = append(status.errors, "legacy 配置路径不是安全的普通目录")
			continue
		}
		if legacyInfo.Mode().Perm()&0o077 != 0 {
			status.warnings = append(status.warnings, fmt.Sprintf("legacy 配置目录 %q 权限 %04o 偏宽（建议 0700）", legacyRoot, legacyInfo.Mode().Perm()))
		}
	}
	metadataWarnings, metadataErrors := inspectDoctorProviderMetadata()
	status.warnings = append(status.warnings, metadataWarnings...)
	status.errors = append(status.errors, metadataErrors...)
	return status
}

func inspectDoctorProviderMetadata() (warnings, problems []string) {
	roots, err := configRootsForReadE()
	if err != nil {
		return nil, []string{err.Error()}
	}
	providerIDs := make([]string, 0)
	seen := make(map[string]bool)

	// Discover provider IDs from all roots. The order mirrors runtime reads:
	// MuxLM first, then ProviderDeck and cx, with duplicate IDs merged.
	for _, configRoot := range roots {
		root := filepath.Join(configRoot, "providers")
		info, err := os.Lstat(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			problems = append(problems, "无法检查 provider 配置目录: "+err.Error())
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			problems = append(problems, fmt.Sprintf("provider 配置路径 %q 不是安全的普通目录", root))
			continue
		}
		if err := validateDoctorDirWithin(root, configRoot); err != nil {
			problems = append(problems, "provider 配置路径不安全: "+err.Error())
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			problems = append(problems, "无法读取 provider 配置目录: "+err.Error())
			continue
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				problems = append(problems, fmt.Sprintf("provider 配置 %q 是符号链接", entry.Name()))
				continue
			}
			if !entry.IsDir() {
				continue
			}
			if safeID(entry.Name()) != entry.Name() {
				problems = append(problems, fmt.Sprintf("provider 配置目录名 %q 无效", entry.Name()))
				continue
			}
			if seen[entry.Name()] {
				continue
			}
			seen[entry.Name()] = true
			providerIDs = append(providerIDs, entry.Name())
			dir := filepath.Join(root, entry.Name())
			if dirInfo, err := os.Lstat(dir); err == nil && dirInfo.Mode().Perm()&0o077 != 0 {
				warnings = append(warnings, fmt.Sprintf("provider 配置目录 %q 权限 %04o 偏宽", entry.Name(), dirInfo.Mode().Perm()))
			}
		}
	}

	for _, id := range providerIDs {
		if path, found, err := resolveDoctorProviderFile(roots, id, "keys.json"); err != nil {
			problems = append(problems, fmt.Sprintf("%s/keys.json 无法检查: %v", id, err))
		} else if found {
			var file keyFile
			if err := readDoctorJSON(path, &file); err != nil {
				problems = append(problems, fmt.Sprintf("%s/keys.json 损坏: %v", id, err))
			} else if file.Version != 1 {
				problems = append(problems, fmt.Sprintf("%s/keys.json version %d 不支持", id, file.Version))
			} else if err := validateKeyRecords(id, file.Keys); err != nil {
				problems = append(problems, fmt.Sprintf("%s/keys.json 无效: %v", id, err))
			}
		}
		if path, found, err := resolveDoctorProviderFile(roots, id, "provider.json"); err != nil {
			problems = append(problems, fmt.Sprintf("%s/provider.json 无法检查: %v", id, err))
		} else if found {
			var file customProviderFile
			if err := readDoctorJSON(path, &file); err != nil {
				problems = append(problems, fmt.Sprintf("%s/provider.json 损坏: %v", id, err))
			} else if file.Version != 1 {
				problems = append(problems, fmt.Sprintf("%s/provider.json version %d 不支持", id, file.Version))
			} else if err := validateStoredCustomProvider(&file.Provider, id); err != nil {
				problems = append(problems, fmt.Sprintf("%s/provider.json 无效: %v", id, err))
			}
		}
		// secrets.json may contain plaintext API keys. Doctor deliberately uses
		// Lstat only: it verifies the effective new-first/legacy-fallback path and
		// permissions without ever opening or parsing secret contents.
		if path, found, err := resolveDoctorProviderFile(roots, id, "secrets.json"); err != nil {
			problems = append(problems, fmt.Sprintf("%s/secrets.json 无法检查: %v", id, err))
		} else if found {
			info, statErr := os.Lstat(path)
			if statErr != nil {
				problems = append(problems, fmt.Sprintf("%s/secrets.json 无法检查: %v", id, statErr))
			} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				problems = append(problems, fmt.Sprintf("%s/secrets.json 不是安全的普通文件", id))
			} else if info.Size() > maxPrivateFileBytes {
				problems = append(problems, fmt.Sprintf("%s/secrets.json 超过 2 MiB 限制", id))
			} else if info.Mode().Perm()&0o177 != 0 {
				warnings = append(warnings, fmt.Sprintf("%s/secrets.json 权限 %04o 偏宽（建议 0600）", id, info.Mode().Perm()))
			}
		}
	}
	return warnings, problems
}

func resolveDoctorFile(name string) (path, root string, rootIndex int, found bool, err error) {
	roots, rootsErr := configRootsForReadE()
	if rootsErr != nil {
		return "", "", 0, false, rootsErr
	}
	for i, candidateRoot := range roots {
		candidate := filepath.Join(candidateRoot, name)
		if _, statErr := os.Lstat(candidate); statErr == nil {
			return candidate, candidateRoot, i, true, nil
		} else if !os.IsNotExist(statErr) {
			return candidate, candidateRoot, i, false, statErr
		}
	}
	return "", "", 0, false, nil
}

func resolveDoctorProviderFile(roots []string, id, name string) (string, bool, error) {
	for _, root := range roots {
		path := filepath.Join(root, "providers", id, name)
		if _, err := os.Lstat(path); err == nil {
			if err := validateDoctorDirWithin(filepath.Dir(path), root); err != nil {
				return path, false, err
			}
			return path, true, nil
		} else if !os.IsNotExist(err) {
			return path, false, err
		}
	}
	return "", false, nil
}

// validateDoctorDirWithin is the read-only counterpart to
// ensurePrivateDirWithin: it validates the existing directory chain without
// creating directories or tightening permissions.
func validateDoctorDirWithin(dir, root string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	cur, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if cur != rootAbs && !strings.HasPrefix(cur, rootAbs+string(os.PathSeparator)) {
		return fmt.Errorf("路径不在配置目录内: %s", cur)
	}
	for check := cur; ; check = filepath.Dir(check) {
		info, statErr := os.Lstat(check)
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("不是安全的普通目录: %s", check)
		}
		if check == rootAbs {
			break
		}
	}
	return nil
}

func isDoctorLegacyRoot(root string, index int) bool {
	if index > 0 {
		return true
	}
	if os.Getenv("MUXLM_CONFIG_DIR") != "" {
		return false
	}
	if firstEnv("PROVIDERDECK_CONFIG_DIR", "CX_CONFIG_DIR") != "" {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil {
		currentAbs, _ := filepath.Abs(filepath.Join(home, ".config", "muxlm"))
		rootAbs, _ := filepath.Abs(root)
		return currentAbs != rootAbs
	}
	return false
}

func readDoctorJSON(path string, target any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("不是安全的普通文件")
	}
	if info.Size() > maxDoctorMetadataBytes {
		return fmt.Errorf("文件超过 2 MiB 限制")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	return nil
}

func doctorBackendWarning(backend string) string {
	var command string
	switch backend {
	case "keychain":
		command = "security"
	case "secret-service":
		command = "secret-tool"
	case "file":
		return ""
	default:
		return fmt.Sprintf("未知密钥后端 %q", backend)
	}
	if _, err := exec.LookPath(command); err != nil {
		return fmt.Sprintf("密钥后端 %s 需要 %s，但它不在 PATH 中", backend, command)
	}
	return ""
}
