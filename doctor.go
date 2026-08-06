package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

type doctorLinuxStatus struct {
	lines    []string
	warnings []string
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
	linux := inspectDoctorLinux(runtime.GOOS, backend)
	for _, line := range linux.lines {
		fmt.Fprintln(w, line)
	}
	warnings = append(warnings, linux.warnings...)

	cliWarnings := 0
	for _, name := range []string{"codex", "claude", "opencode"} {
		path, err := exec.LookPath(name)
		if err != nil {
			fmt.Fprintf(w, tr("%-9s ⚠ 未找到\n", "%-9s ⚠ not found\n"), name)
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
		return fmt.Errorf(tr("doctor 检测到 %d 个 catalog/配置错误", "doctor found %d catalog/configuration errors"), len(problems))
	}
	fmt.Fprintf(w, "status    ✓ OK (%d warning(s))\n", warningCount)
	return nil
}

func inspectDoctorCatalog() doctorCatalogStatus {
	status := doctorCatalogStatus{revision: embeddedCatalog.Revision, origin: "embedded"}
	if err := validateCatalog(&embeddedCatalog); err != nil {
		status.errors = append(status.errors, tr("内置 catalog 无效: ", "embedded catalog is invalid: ")+err.Error())
		return status
	}
	if _, err := validateUpdateURL(catalogURL()); err != nil {
		status.errors = append(status.errors, tr("catalog source 无效: ", "invalid catalog source: ")+err.Error())
	}

	path, root, rootIndex, found, err := resolveDoctorFile("catalog.json")
	if err != nil {
		status.errors = append(status.errors, tr("无法检查 catalog cache: ", "could not inspect catalog cache: ")+err.Error())
		return status
	}
	if !found {
		return status
	}
	status.cache = path
	if err := validateDoctorDirWithin(filepath.Dir(path), root); err != nil {
		status.errors = append(status.errors, tr("catalog cache 路径不安全: ", "unsafe catalog cache path: ")+err.Error())
		return status
	}
	info, err := os.Lstat(path)
	if err != nil {
		status.errors = append(status.errors, tr("无法检查 catalog cache: ", "could not inspect catalog cache: ")+err.Error())
		return status
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		status.errors = append(status.errors, tr("catalog cache 不是安全的普通文件", "catalog cache is not a safe regular file"))
		return status
	}
	if info.Size() > maxCatalogBytes {
		status.errors = append(status.errors, tr("catalog cache 超过 2 MiB 限制", "catalog cache exceeds the 2 MiB limit"))
		return status
	}
	data, err := os.ReadFile(path)
	if err != nil {
		status.errors = append(status.errors, tr("无法读取 catalog cache: ", "could not read catalog cache: ")+err.Error())
		return status
	}
	cached, err := decodeCatalog(data)
	if err != nil {
		status.errors = append(status.errors, tr("catalog cache 损坏: ", "catalog cache is corrupt: ")+err.Error())
		return status
	}
	if compareCatalogRevision(cached.Revision, embeddedCatalog.Revision) < 0 {
		status.warnings = append(status.warnings, fmt.Sprintf(tr("catalog cache %s 旧于内置版本 %s，已忽略", "catalog cache %s is older than embedded revision %s and was ignored"), cached.Revision, embeddedCatalog.Revision))
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
		status.errors = append(status.errors, tr("无法检查配置目录: ", "could not inspect configuration directory: ")+err.Error())
		return status
	}
	status.detail = fmt.Sprintf("mode %04o", info.Mode().Perm())
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		status.errors = append(status.errors, tr("配置路径不是安全的普通目录", "configuration path is not a safe regular directory"))
		return status
	}
	if info.Mode().Perm()&0o077 != 0 {
		status.warnings = append(status.warnings, fmt.Sprintf(tr("配置目录权限 %04o 偏宽（建议 0700）", "configuration directory permissions %04o are too broad (recommended: 0700)"), info.Mode().Perm()))
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
			} else {
				for _, warning := range doctorStoredKeyBackendWarnings(file.Keys, runtime.GOOS) {
					warnings = append(warnings, fmt.Sprintf("%s/keys.json：%s", id, warning))
				}
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

func doctorStoredKeyBackendWarnings(keys []KeyRecord, goos string) []string {
	usesKeychain, usesSecretService := false, false
	for _, key := range keys {
		usesKeychain = usesKeychain || key.Backend == "keychain"
		usesSecretService = usesSecretService || key.Backend == "secret-service"
	}
	var warnings []string
	if usesKeychain && goos != "darwin" {
		warnings = append(warnings, tr("有些 key 保存在 macOS Keychain，当前系统无法读取；请通过环境变量提供，或在本机重新保存", "Some keys are stored in macOS Keychain and cannot be read on this system; provide them through environment variables or save them again locally"))
	}
	if usesSecretService {
		switch {
		case goos != "linux":
			warnings = append(warnings, tr("有些 key 保存在 Linux Secret Service，当前系统无法读取；请通过环境变量提供，或在本机重新保存", "Some keys are stored in Linux Secret Service and cannot be read on this system; provide them through environment variables or save them again locally"))
		case !visibleSessionBus(os.Getenv):
			warnings = append(warnings, tr("有些 key 保存在 Secret Service，但当前没有桌面 D-Bus 会话；请回到有密钥环的会话，或通过环境变量提供", "Some keys are stored in Secret Service, but no desktop D-Bus session is available; use a keyring session or provide them through environment variables"))
		default:
			if _, err := exec.LookPath("secret-tool"); err != nil {
				warnings = append(warnings, tr("有些 key 保存在 Secret Service，但 PATH 中没有 secret-tool；请安装后重试", "Some keys are stored in Secret Service, but secret-tool is not in PATH; install it and retry"))
			}
		}
	}
	return warnings
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
		return fmt.Errorf(tr("路径不在配置目录内: %s", "path is outside the configuration directory: %s"), cur)
	}
	for check := cur; ; check = filepath.Dir(check) {
		info, statErr := os.Lstat(check)
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf(tr("不是安全的普通目录: %s", "not a safe regular directory: %s"), check)
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
		return fmt.Errorf(tr("不是安全的普通文件", "not a safe regular file"))
	}
	if info.Size() > maxDoctorMetadataBytes {
		return fmt.Errorf(tr("文件超过 2 MiB 限制", "file exceeds the 2 MiB limit"))
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
		return fmt.Sprintf(tr("未知密钥后端 %q", "unknown secret backend %q"), backend)
	}
	if _, err := exec.LookPath(command); err != nil {
		return fmt.Sprintf(tr("密钥后端 %s 需要 %s，但它不在 PATH 中", "secret backend %s requires %s, but it is not in PATH"), backend, command)
	}
	return ""
}

// inspectDoctorLinux only examines environment variables, PATH entries, and
// local file metadata. It deliberately does not invoke secret-tool, contact
// D-Bus, read a stored secret, or make a network request.
func inspectDoctorLinux(goos, backend string) doctorLinuxStatus {
	if goos != "linux" {
		return doctorLinuxStatus{}
	}

	status := doctorLinuxStatus{}
	appendCommand := func(label, command, purpose string) {
		path, err := exec.LookPath(command)
		if err == nil {
			status.lines = append(status.lines, fmt.Sprintf("%-9s ✓ %q", label, path))
			return
		}
		status.lines = append(status.lines, fmt.Sprintf(tr("%-9s ⚠ 未找到（%s）", "%-9s ⚠ not found (%s)"), label, purpose))
		status.warnings = append(status.warnings, fmt.Sprintf(tr("PATH 中没有 %s；%s", "%s is not in PATH; %s"), command, purpose))
	}
	appendCommand("bash", "bash", tr("MuxLM 自更新需要 bash", "bash is required for MuxLM self-update"))
	appendCommand("curl", "curl", tr("安装和自更新需要 curl", "curl is required for installation and self-update"))

	if path, err := exec.LookPath("sha256sum"); err == nil {
		status.lines = append(status.lines, fmt.Sprintf("%-9s ✓ %q", tr("文件校验", "checksum"), path))
	} else if path, err := exec.LookPath("shasum"); err == nil {
		status.lines = append(status.lines, fmt.Sprintf("%-9s ✓ %q", tr("文件校验", "checksum"), path))
	} else {
		status.lines = append(status.lines, fmt.Sprintf(tr("%-9s ⚠ 未找到（需要 sha256sum 或 shasum）", "%-9s ⚠ not found (sha256sum or shasum required)"), tr("文件校验", "checksum")))
		status.warnings = append(status.warnings, tr("PATH 中没有 sha256sum 或 shasum；安装器无法校验下载文件", "sha256sum and shasum are missing from PATH; the installer cannot verify downloads"))
	}

	configuredBackend := strings.ToLower(firstEnv(
		"MUXLM_SECRET_BACKEND",
		"PROVIDERDECK_SECRET_BACKEND",
		"CX_SECRET_BACKEND",
	))
	switch backend {
	case "secret-service":
		status.lines = append(status.lines, tr("密钥存储  系统密钥环（Secret Service）", "secrets    system keyring (Secret Service)"))
		if !visibleSessionBus(os.Getenv) {
			status.warnings = append(status.warnings,
				tr("未发现桌面 D-Bus 会话，Secret Service 可能不可用；无桌面服务器请设置 MUXLM_SECRET_BACKEND=file", "No desktop D-Bus session was found; Secret Service may be unavailable. On headless servers, set MUXLM_SECRET_BACKEND=file"))
		}
	case "file":
		status.lines = append(status.lines, tr("密钥存储  本地文件（权限 0600，适合无桌面 Linux）", "secrets    local file (mode 0600; suitable for headless Linux)"))
		if configuredBackend == "" {
			status.warnings = append(status.warnings,
				tr("当前自动使用本地密钥文件；无桌面服务器建议设置 MUXLM_SECRET_BACKEND=file，避免环境变化后切换后端", "Local secret files are selected automatically; on headless servers, set MUXLM_SECRET_BACKEND=file to keep the backend stable"))
		}
	}

	if line, warning := doctorLinuxUserBin(); line != "" {
		status.lines = append(status.lines, line)
		if warning != "" {
			status.warnings = append(status.warnings, warning)
		}
	}
	return status
}

func doctorLinuxUserBin() (line, warning string) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", ""
	}
	userBin := filepath.Join(home, ".local", "bin")
	hasMuxLMEntry := false
	for _, name := range []string{"muxlm", "cdx", "cld", "opc"} {
		if _, err := os.Lstat(filepath.Join(userBin, name)); err == nil {
			hasMuxLMEntry = true
			break
		}
	}
	if !hasMuxLMEntry {
		return "", ""
	}
	if pathContainsDir(os.Getenv("PATH"), userBin) {
		return fmt.Sprintf("%-9s ✓ %q", tr("用户命令", "commands"), userBin), ""
	}
	return fmt.Sprintf(tr("%-9s ⚠ %q 不在 PATH 中", "%-9s ⚠ %q is not in PATH"), tr("用户命令", "commands"), userBin),
		fmt.Sprintf(tr(`%s 中有 MuxLM 命令但目录不在 PATH；请执行 export PATH="%s:$PATH"`, `MuxLM commands exist in %s, but the directory is not in PATH; run export PATH="%s:$PATH"`), userBin, userBin)
}

func pathContainsDir(pathValue, dir string) bool {
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	dirAbs = filepath.Clean(dirAbs)
	for _, entry := range filepath.SplitList(pathValue) {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		entryAbs, err := filepath.Abs(entry)
		if err == nil && filepath.Clean(entryAbs) == dirAbs {
			return true
		}
	}
	return false
}
