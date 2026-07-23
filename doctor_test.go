package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func setupDoctorTest(t *testing.T, commands ...string) (configDirPath, binDir string) {
	t.Helper()
	root := t.TempDir()
	configDirPath = filepath.Join(root, "config")
	binDir = filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	commands = append([]string{"bash", "curl", "sha256sum"}, commands...)
	created := make(map[string]bool)
	for _, command := range commands {
		if created[command] {
			continue
		}
		created[command] = true
		path := filepath.Join(binDir, command)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("MUXLM_CONFIG_DIR", configDirPath)
	t.Setenv("PROVIDERDECK_CONFIG_DIR", "")
	t.Setenv("CX_CONFIG_DIR", "")
	t.Setenv("MUXLM_SECRET_BACKEND", "file")
	t.Setenv("PROVIDERDECK_SECRET_BACKEND", "")
	t.Setenv("CX_SECRET_BACKEND", "")
	t.Setenv("MUXLM_CATALOG_URL", defaultCatalogURL)
	t.Setenv("PROVIDERDECK_CATALOG_URL", "")
	t.Setenv("CX_CATALOG_URL", "")
	t.Setenv("PATH", binDir)
	return configDirPath, binDir
}

func setupDoctorHomeTest(t *testing.T, commands ...string) (current, providerDeck, cx string) {
	t.Helper()
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	commands = append([]string{"bash", "curl", "sha256sum"}, commands...)
	created := make(map[string]bool)
	for _, command := range commands {
		if created[command] {
			continue
		}
		created[command] = true
		path := filepath.Join(binDir, command)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("MUXLM_CONFIG_DIR", "")
	t.Setenv("PROVIDERDECK_CONFIG_DIR", "")
	t.Setenv("CX_CONFIG_DIR", "")
	t.Setenv("MUXLM_SECRET_BACKEND", "file")
	t.Setenv("PROVIDERDECK_SECRET_BACKEND", "")
	t.Setenv("CX_SECRET_BACKEND", "")
	t.Setenv("MUXLM_CATALOG_URL", defaultCatalogURL)
	t.Setenv("PROVIDERDECK_CATALOG_URL", "")
	t.Setenv("CX_CATALOG_URL", "")
	t.Setenv("PATH", binDir)
	return filepath.Join(home, ".config", "muxlm"),
		filepath.Join(home, ".config", "providerdeck"),
		filepath.Join(home, ".config", "cx")
}

func writeDoctorTestJSON(t *testing.T, path string, value any, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorReportsLocalStateWithoutNetwork(t *testing.T) {
	config, _ := setupDoctorTest(t, "codex", "claude", "opencode")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	t.Setenv("MUXLM_CATALOG_URL", server.URL)

	var out bytes.Buffer
	if err := runDoctor(&out); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatalf("doctor made %d network request(s)", requests.Load())
	}
	text := out.String()
	for _, want := range []string{
		appName + " " + appVersion,
		"catalog   " + embeddedCatalog.Revision + " (embedded)",
		fmt.Sprintf("source    %q", server.URL),
		fmt.Sprintf("config    %q (not created)", config),
		"secrets   file",
		"codex     ✓",
		"claude    ✓",
		"opencode  ✓",
		"status    ✓ OK (0 warning(s))",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, text)
		}
	}
}

func TestDoctorMissingCLIsAreWarnings(t *testing.T) {
	setupDoctorTest(t)
	var out bytes.Buffer
	if err := runDoctor(&out); err != nil {
		t.Fatalf("missing optional CLIs returned an error: %v\n%s", err, out.String())
	}
	if got := strings.Count(out.String(), "⚠ not found"); got != 3 {
		t.Fatalf("missing CLI warnings=%d:\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "status    ✓ OK (3 warning(s))") {
		t.Fatalf("unexpected status:\n%s", out.String())
	}
}

func TestDoctorLinuxExplainsDependenciesFileBackendAndUserPath(t *testing.T) {
	_, bin := setupDoctorTest(t)
	userBin := filepath.Join(os.Getenv("HOME"), ".local", "bin")
	if err := os.MkdirAll(userBin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userBin, "cld"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	status := inspectDoctorLinux("linux", "file")
	text := strings.Join(status.lines, "\n") + "\n" + strings.Join(status.warnings, "\n")
	for _, want := range []string{
		"bash      ✓",
		"curl      ✓",
		"文件校验",
		"sha256sum",
		"密钥存储  本地文件（权限 0600，适合无桌面 Linux）",
		fmt.Sprintf(`%q 不在 PATH 中`, userBin),
		fmt.Sprintf(`export PATH="%s:$PATH"`, userBin),
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Linux doctor output missing %q:\n%s", want, text)
		}
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+userBin)
	status = inspectDoctorLinux("linux", "file")
	text = strings.Join(status.lines, "\n") + "\n" + strings.Join(status.warnings, "\n")
	if !strings.Contains(text, "用户命令") ||
		!strings.Contains(text, fmt.Sprintf(`✓ %q`, userBin)) ||
		strings.Contains(text, "不在 PATH") {
		t.Fatalf("Linux doctor did not recognize the user bin PATH entry:\n%s", text)
	}
}

func TestDoctorLinuxReportsMissingInstallDependencies(t *testing.T) {
	setupDoctorTest(t)
	emptyBin := t.TempDir()
	t.Setenv("PATH", emptyBin)

	status := inspectDoctorLinux("linux", "file")
	text := strings.Join(status.lines, "\n") + "\n" + strings.Join(status.warnings, "\n")
	for _, want := range []string{
		"bash      ⚠ 未找到",
		"curl      ⚠ 未找到",
		"文件校验",
		"需要 sha256sum 或 shasum",
		"安装器无法校验下载文件",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Linux dependency guidance missing %q:\n%s", want, text)
		}
	}
	if len(status.warnings) != 3 {
		t.Fatalf("Linux dependency warning count=%d, want 3:\n%s", len(status.warnings), text)
	}
}

func TestDoctorLinuxDoesNotRunSecretTool(t *testing.T) {
	_, bin := setupDoctorTest(t, "secret-tool")
	marker := filepath.Join(t.TempDir(), "secret-tool-ran")
	script := fmt.Sprintf("#!/bin/sh\n: > %q\nexit 0\n", marker)
	if err := os.WriteFile(filepath.Join(bin, "secret-tool"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUXLM_SECRET_BACKEND", "secret-service")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus")

	var out bytes.Buffer
	if err := runDoctor(&out); err != nil {
		t.Fatalf("doctor failed: %v\n%s", err, out.String())
	}
	status := inspectDoctorLinux("linux", "secret-service")
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("doctor invoked secret-tool: %v", err)
	}
	text := strings.Join(status.lines, "\n") + "\n" + strings.Join(status.warnings, "\n")
	if !strings.Contains(text, "密钥存储  系统密钥环（Secret Service）") ||
		strings.Contains(text, "D-Bus") {
		t.Fatalf("unexpected Secret Service guidance:\n%s", text)
	}
}

func TestDoctorLinuxRecognizesRuntimeBusWithoutContactingIt(t *testing.T) {
	setupDoctorTest(t, "secret-tool")
	t.Setenv("MUXLM_SECRET_BACKEND", "secret-service")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	runtimeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(runtimeDir, "bus"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)

	status := inspectDoctorLinux("linux", "secret-service")
	if strings.Contains(strings.Join(status.warnings, "\n"), "D-Bus") {
		t.Fatalf("existing XDG runtime bus was ignored: %#v", status)
	}
}

func TestDoctorWarnsWhenStoredSecretBackendIsUnavailable(t *testing.T) {
	config, _ := setupDoctorTest(t)
	writeDoctorTestJSON(t, filepath.Join(config, "providers", "kimi", "keys.json"), keyFile{
		Version: 1,
		Keys: []KeyRecord{{
			ID:      "key1",
			Name:    "key1",
			Plan:    "coding",
			Region:  "cn",
			Backend: "secret-service",
			Ref:     "provider/kimi/key/key1",
		}},
	}, 0o600)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", "")

	var out bytes.Buffer
	if err := runDoctor(&out); err != nil {
		t.Fatalf("doctor failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "kimi/keys.json：有些 key 保存在") ||
		!strings.Contains(out.String(), "Secret Service") ||
		(!strings.Contains(out.String(), "当前没有桌面 D-Bus 会话") &&
			!strings.Contains(out.String(), "当前系统无法读取")) {
		t.Fatalf("doctor did not explain the unavailable stored backend:\n%s", out.String())
	}
	if warnings := strings.Join(doctorStoredKeyBackendWarnings([]KeyRecord{{
		Backend: "secret-service",
	}}, "linux"), "\n"); !strings.Contains(warnings, "当前没有桌面 D-Bus 会话") {
		t.Fatalf("Linux headless backend warning missing: %s", warnings)
	}
}

func TestDoctorRejectsCorruptCatalogCache(t *testing.T) {
	config, _ := setupDoctorTest(t, "codex", "claude", "opencode")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "catalog.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runDoctor(&out); err == nil {
		t.Fatalf("corrupt catalog was accepted:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "catalog cache 损坏") || !strings.Contains(out.String(), "status    ✗ 1 error(s)") {
		t.Fatalf("corrupt catalog was not explained:\n%s", out.String())
	}
}

func TestDoctorRejectsCorruptConfigMetadataWithoutReadingSecrets(t *testing.T) {
	setupDoctorTest(t, "codex", "claude", "opencode")
	dir := providerDir("minimax")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providerKeysFile("minimax"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	// This file may contain fallback plaintext keys. Doctor must not parse it.
	if err := os.WriteFile(fileSecretsPath("minimax"), []byte("not-json-and-must-not-be-read"), 0o000); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runDoctor(&out); err == nil {
		t.Fatalf("corrupt key metadata was accepted:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "minimax/keys.json 损坏") || strings.Contains(out.String(), "secrets.json") {
		t.Fatalf("doctor inspected the wrong configuration data:\n%s", out.String())
	}

	if err := os.Remove(providerKeysFile("minimax")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runDoctor(&out); err != nil {
		t.Fatalf("unreadable secret contents affected doctor: %v\n%s", err, out.String())
	}
}

func TestDoctorLstatChecksEffectiveSecretStoreWithoutReadingIt(t *testing.T) {
	current, legacy, _ := setupDoctorHomeTest(t, "codex", "claude", "opencode")
	id := "custom-secretcheck"
	currentDir := filepath.Join(current, "providers", id)
	legacyDir := filepath.Join(legacy, "providers", id)
	for _, dir := range []string{currentDir, legacyDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	currentSecret := filepath.Join(currentDir, "secrets.json")
	legacySecret := filepath.Join(legacyDir, "secrets.json")
	// Invalid JSON proves doctor does not parse the secret store. The primary
	// 0600 file must also shadow the overly broad legacy file.
	if err := os.WriteFile(currentSecret, []byte("not-json-and-must-not-be-read"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacySecret, []byte("also-not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(legacySecret, 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runDoctor(&out); err != nil {
		t.Fatalf("secret contents were read: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "secrets.json 权限") || strings.Contains(out.String(), "not-json") {
		t.Fatalf("shadowed secret metadata or contents affected doctor:\n%s", out.String())
	}

	if err := os.Remove(currentSecret); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runDoctor(&out); err != nil {
		t.Fatalf("wide legacy secret permissions should warn, not fail: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), id+"/secrets.json 权限 0644 偏宽") || strings.Contains(out.String(), "not-json") {
		t.Fatalf("effective legacy secret permissions were not reported safely:\n%s", out.String())
	}

	if err := os.Mkdir(currentSecret, 0o700); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runDoctor(&out); err == nil || !strings.Contains(out.String(), id+"/secrets.json 不是安全的普通文件") {
		t.Fatalf("special primary secret store was not rejected:\n%s", out.String())
	}

	if err := os.Remove(currentSecret); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentSecret, []byte{0}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(currentSecret, maxPrivateFileBytes+1); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runDoctor(&out); err == nil || !strings.Contains(out.String(), id+"/secrets.json 超过 2 MiB 限制") {
		t.Fatalf("oversized primary secret store was not rejected:\n%s", out.String())
	}
}

func TestDoctorReportsPermissionsWithoutChangingThem(t *testing.T) {
	config, _ := setupDoctorTest(t, "codex", "claude", "opencode")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config, 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runDoctor(&out); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("doctor changed config permissions to %04o", info.Mode().Perm())
	}
	if !strings.Contains(out.String(), "config    "+fmt.Sprintf("%q", config)+" (mode 0755)") ||
		!strings.Contains(out.String(), "建议 0700") {
		t.Fatalf("permission warning missing:\n%s", out.String())
	}
}

func TestDoctorUsesAndReportsLegacyCatalogFallbackReadOnly(t *testing.T) {
	for _, legacyName := range []string{"providerdeck", "cx"} {
		t.Run(legacyName, func(t *testing.T) {
			current, providerDeck, cx := setupDoctorHomeTest(t, "codex", "claude", "opencode")
			legacy := providerDeck
			if legacyName == "cx" {
				legacy = cx
			}
			if err := os.MkdirAll(current, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(legacy, 0o700); err != nil {
				t.Fatal(err)
			}
			cached := cloneCatalog(t, &embeddedCatalog)
			cached.Revision = "2099-01-01.1"
			cachePath := filepath.Join(legacy, "catalog.json")
			writeDoctorTestJSON(t, cachePath, cached, 0o644)
			if err := os.Chmod(cachePath, 0o644); err != nil {
				t.Fatal(err)
			}

			var out bytes.Buffer
			if err := runDoctor(&out); err != nil {
				t.Fatalf("legacy cache should be valid: %v\n%s", err, out.String())
			}
			text := out.String()
			if !strings.Contains(text, "catalog   2099-01-01.1 (legacy cache)") ||
				!strings.Contains(text, fmt.Sprintf("cache     %q", cachePath)) {
				t.Fatalf("actual legacy cache source was not reported:\n%s", text)
			}
			if info, err := os.Stat(cachePath); err != nil {
				t.Fatalf("legacy cache disappeared: %v", err)
			} else if info.Mode().Perm() != 0o644 {
				t.Fatalf("doctor changed legacy cache permissions: mode=%04o", info.Mode().Perm())
			}
			if _, err := os.Lstat(filepath.Join(current, "catalog.json")); !os.IsNotExist(err) {
				t.Fatalf("doctor created a primary cache: %v", err)
			}
		})
	}
}

func TestDoctorMergesProviderMetadataWithPerFileFallback(t *testing.T) {
	current, legacy, _ := setupDoctorHomeTest(t, "codex", "claude", "opencode")
	currentProviders := filepath.Join(current, "providers")
	legacyProviders := filepath.Join(legacy, "providers")
	for _, dir := range []string{
		filepath.Join(currentProviders, "custom-shared"),
		filepath.Join(legacyProviders, "custom-shared"),
		filepath.Join(currentProviders, "custom-keys"),
		filepath.Join(legacyProviders, "custom-keys"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	validKeys := keyFile{Version: 1, Keys: []KeyRecord{{
		ID: "main", Name: "main", Plan: "custom", Region: "cn", Backend: "file",
		Ref: "provider/custom-shared/key/main",
	}}}
	writeDoctorTestJSON(t, filepath.Join(currentProviders, "custom-shared", "keys.json"), validKeys, 0o644)
	if err := os.WriteFile(filepath.Join(legacyProviders, "custom-shared", "keys.json"), []byte("{shadowed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyProviders, "custom-shared", "provider.json"), []byte("{broken"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(currentProviders, "custom-shared", "secrets.json"), []byte("must-not-be-read"), 0o000); err != nil {
		t.Fatal(err)
	}

	provider := Provider{
		ID: "custom-keys", Alias: "keys", Name: "Keys", Plan: "custom",
		OpenAIURL: "https://example.com/v1", KeyEnv: "PROVIDERDECK_KEYS_KEY",
		CLI: []string{"codex"}, Models: []Model{{ID: "model", Latest: true}},
	}
	writeDoctorTestJSON(t, filepath.Join(currentProviders, "custom-keys", "provider.json"), customProviderFile{Version: 1, Provider: provider}, 0o644)
	if err := os.WriteFile(filepath.Join(legacyProviders, "custom-keys", "provider.json"), []byte("{shadowed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyProviders, "custom-keys", "keys.json"), []byte("{broken"), 0o640); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runDoctor(&out); err == nil {
		t.Fatalf("effective corrupt legacy metadata was accepted:\n%s", out.String())
	}
	text := out.String()
	if strings.Count(text, "custom-shared/provider.json 损坏") != 1 ||
		strings.Count(text, "custom-keys/keys.json 损坏") != 1 {
		t.Fatalf("effective legacy metadata was not checked exactly once:\n%s", text)
	}
	if strings.Contains(text, "custom-shared/keys.json 损坏") ||
		strings.Contains(text, "custom-keys/provider.json 损坏") || strings.Contains(text, "secrets.json") {
		t.Fatalf("doctor checked shadowed metadata or secret contents:\n%s", text)
	}
	for _, path := range []string{
		filepath.Join(currentProviders, "custom-shared"),
		filepath.Join(legacyProviders, "custom-shared"),
	} {
		if info, err := os.Stat(path); err != nil {
			t.Fatalf("provider directory %s disappeared: %v", path, err)
		} else if info.Mode().Perm() != 0o755 {
			t.Fatalf("doctor changed provider directory %s: mode=%04o", path, info.Mode().Perm())
		}
	}
}

func TestRunRemoveRejectsSymlinkedProvidersRoot(t *testing.T) {
	config, _ := setupDoctorTest(t, "codex", "claude", "opencode")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	target := filepath.Join(external, "minimax")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(config, "providers")); err != nil {
		t.Fatal(err)
	}

	var removeErr error
	withStdin(t, "yes\n", func() { removeErr = runRemove("m") })
	if removeErr == nil || !strings.Contains(removeErr.Error(), "provider 配置目录") {
		t.Fatalf("symlinked providers root was not rejected: %v", removeErr)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatalf("external provider data was changed: %q, %v", data, err)
	}
}

func TestDoctorIsAReservedAlias(t *testing.T) {
	if !isReservedAlias("doctor") {
		t.Fatal("doctor must not be accepted as a provider/model alias")
	}
}
