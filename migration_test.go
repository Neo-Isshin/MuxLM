package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMuxLMEnvironmentTakesPrecedence(t *testing.T) {
	t.Setenv("MUXLM_CATALOG_URL", "https://canonical.example/catalog.json")
	t.Setenv("PROVIDERDECK_CATALOG_URL", "https://primary.example/catalog.json")
	t.Setenv("CX_CATALOG_URL", "https://legacy.example/catalog.json")
	if got := catalogURL(); got != "https://canonical.example/catalog.json" {
		t.Fatalf("catalog URL = %q", got)
	}

	t.Setenv("MUXLM_RELEASE_API_URL", "https://canonical.example/releases/latest")
	t.Setenv("PROVIDERDECK_RELEASE_API_URL", "https://primary.example/releases/latest")
	t.Setenv("CX_RELEASE_API_URL", "https://legacy.example/releases/latest")
	if got := releaseAPIURL(); got != "https://canonical.example/releases/latest" {
		t.Fatalf("release API URL = %q", got)
	}

	t.Setenv("MUXLM_INSTALL_URL", "https://canonical.example/install.sh")
	t.Setenv("PROVIDERDECK_INSTALL_URL", "https://primary.example/install.sh")
	t.Setenv("CX_INSTALL_URL", "https://legacy.example/install.sh")
	if got := installURL(); got != "https://canonical.example/install.sh" {
		t.Fatalf("install URL = %q", got)
	}

	t.Setenv("MUXLM_UPDATE_INTERVAL", "5m")
	t.Setenv("PROVIDERDECK_UPDATE_INTERVAL", "15m")
	t.Setenv("CX_UPDATE_INTERVAL", "1h")
	if got := startupUpdateInterval(); got != 5*time.Minute {
		t.Fatalf("update interval = %v", got)
	}

	t.Setenv("MUXLM_RELEASE_INTERVAL", "10m")
	t.Setenv("PROVIDERDECK_RELEASE_INTERVAL", "30m")
	t.Setenv("CX_RELEASE_INTERVAL", "2h")
	if got := releaseUpdateInterval(); got != 10*time.Minute {
		t.Fatalf("release interval = %v", got)
	}

	canonicalConfig := filepath.Join(t.TempDir(), "muxlm")
	primaryConfig := filepath.Join(t.TempDir(), "providerdeck")
	t.Setenv("MUXLM_CONFIG_DIR", canonicalConfig)
	t.Setenv("PROVIDERDECK_CONFIG_DIR", primaryConfig)
	t.Setenv("CX_CONFIG_DIR", filepath.Join(t.TempDir(), "cx"))
	if got := configDir(); got != canonicalConfig {
		t.Fatalf("config dir = %q", got)
	}

	t.Setenv("MUXLM_SECRET_BACKEND", "file")
	t.Setenv("PROVIDERDECK_SECRET_BACKEND", "keychain")
	t.Setenv("CX_SECRET_BACKEND", "secret-service")
	if got := secretBackend(); got != "file" {
		t.Fatalf("secret backend = %q", got)
	}
}

func TestProviderDeckAndCXEnvironmentFallback(t *testing.T) {
	t.Setenv("MUXLM_CATALOG_URL", "")
	t.Setenv("PROVIDERDECK_CATALOG_URL", "https://providerdeck.example/catalog.json")
	t.Setenv("CX_CATALOG_URL", "https://cx.example/catalog.json")
	if got := catalogURL(); got != "https://providerdeck.example/catalog.json" {
		t.Fatalf("ProviderDeck catalog URL = %q", got)
	}

	t.Setenv("PROVIDERDECK_CATALOG_URL", "")
	if got := catalogURL(); got != "https://cx.example/catalog.json" {
		t.Fatalf("cx catalog URL = %q", got)
	}
	t.Setenv("MUXLM_SECRET_BACKEND", "")
	t.Setenv("PROVIDERDECK_SECRET_BACKEND", "keychain")
	t.Setenv("CX_SECRET_BACKEND", "file")
	if got := secretBackend(); got != "keychain" {
		t.Fatalf("ProviderDeck secret backend = %q", got)
	}
	t.Setenv("PROVIDERDECK_SECRET_BACKEND", "")
	if got := secretBackend(); got != "file" {
		t.Fatalf("cx secret backend = %q", got)
	}

	providerDeckConfig := filepath.Join(t.TempDir(), "providerdeck")
	cxConfig := filepath.Join(t.TempDir(), "cx")
	t.Setenv("MUXLM_CONFIG_DIR", "")
	t.Setenv("PROVIDERDECK_CONFIG_DIR", providerDeckConfig)
	t.Setenv("CX_CONFIG_DIR", cxConfig)
	if got := configDir(); got != providerDeckConfig {
		t.Fatalf("ProviderDeck config dir = %q", got)
	}
	t.Setenv("PROVIDERDECK_CONFIG_DIR", "")
	if got := configDir(); got != cxConfig {
		t.Fatalf("cx config dir = %q", got)
	}
}

func TestConfigDirReusesLegacyUntilNewDirectoryExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("MUXLM_CONFIG_DIR", "")
	t.Setenv("PROVIDERDECK_CONFIG_DIR", "")
	t.Setenv("CX_CONFIG_DIR", "")
	cxRoot := filepath.Join(home, ".config", "cx")
	providerDeckRoot := filepath.Join(home, ".config", "providerdeck")
	muxLMRoot := filepath.Join(home, ".config", "muxlm")
	if err := os.MkdirAll(cxRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cxRoot, "keys.env"), []byte("MINIMAX_KEY=cx-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := configDir(); got != cxRoot {
		t.Fatalf("cx config was not reused: %q", got)
	}
	if err := os.MkdirAll(providerDeckRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := configDir(); got != providerDeckRoot {
		t.Fatalf("ProviderDeck config did not take precedence over cx: %q", got)
	}
	if got := loadLegacyKeys()["MINIMAX_KEY"]; got != "cx-value" {
		t.Fatalf("missing ProviderDeck file did not fall back to cx: %q", got)
	}
	if err := os.WriteFile(filepath.Join(providerDeckRoot, "keys.env"), []byte("MINIMAX_KEY=providerdeck-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(muxLMRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := configDir(); got != muxLMRoot {
		t.Fatalf("MuxLM config did not take precedence: %q", got)
	}
	if got := loadLegacyKeys()["MINIMAX_KEY"]; got != "providerdeck-value" {
		t.Fatalf("missing MuxLM file did not fall back to ProviderDeck: %q", got)
	}
	if err := os.WriteFile(filepath.Join(muxLMRoot, "keys.env"), []byte("MINIMAX_KEY=muxlm-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadLegacyKeys()["MINIMAX_KEY"]; got != "muxlm-value" {
		t.Fatalf("MuxLM file did not override legacy config: %q", got)
	}
}

func TestMissingHomeNeverFallsBackToRelativeConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME behavior is specific to supported Unix targets")
	}
	work := t.TempDir()
	oldWork, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWork) })
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("MUXLM_CONFIG_DIR", "")
	t.Setenv("PROVIDERDECK_CONFIG_DIR", "")
	t.Setenv("CX_CONFIG_DIR", "")
	t.Setenv("MUXLM_SECRET_BACKEND", "file")

	if _, err := configRootsForReadE(); err == nil {
		t.Fatal("missing HOME unexpectedly produced a default config root")
	}
	if got := configDir(); !filepath.IsAbs(got) {
		t.Fatalf("unavailable config sentinel is relative: %q", got)
	}
	if err := atomicWriteJSON(filepath.Join(configDir(), "probe.json"), map[string]bool{"write": true}); err == nil {
		t.Fatal("write succeeded without a resolvable home directory")
	}
	if status := inspectDoctorConfig(); status.detail != "unavailable" || len(status.errors) != 1 {
		t.Fatalf("doctor did not report the unavailable config root: %#v", status)
	}
	if _, err := os.Lstat(filepath.Join(work, ".config")); !os.IsNotExist(err) {
		t.Fatalf("missing HOME created configuration in the working directory: %v", err)
	}
	t.Setenv("HOME", ".")
	if _, err := configRootsForReadE(); err == nil {
		t.Fatal("relative HOME unexpectedly produced a config root")
	}
	if err := atomicWriteJSON(filepath.Join(configDir(), "probe.json"), map[string]bool{"write": true}); err == nil {
		t.Fatal("write succeeded with a relative HOME")
	}
	if _, err := os.Lstat(filepath.Join(work, ".config")); !os.IsNotExist(err) {
		t.Fatalf("relative HOME created configuration in the working directory: %v", err)
	}
}

func TestDualRootProviderKeysAndFileSecretsMigrateOnWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("MUXLM_CONFIG_DIR", "")
	t.Setenv("PROVIDERDECK_CONFIG_DIR", "")
	t.Setenv("CX_CONFIG_DIR", "")
	t.Setenv("MUXLM_SECRET_BACKEND", "file")
	legacy := filepath.Join(home, ".config", "providerdeck")
	current := filepath.Join(home, ".config", "muxlm")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}

	provider := Provider{
		ID: "custom-migrate", Alias: "migrate", Name: "Legacy", Plan: "custom",
		OpenAIURL: "https://example.com/v1", KeyEnv: "PROVIDERDECK_MIGRATE_KEY",
		CLI: []string{"codex"}, Models: []Model{{ID: "model", Latest: true}},
	}
	if err := atomicWriteJSON(customProviderPath(provider.ID), customProviderFile{Version: 1, Provider: provider}); err != nil {
		t.Fatal(err)
	}
	oldRef := "provider/custom-migrate/key/old"
	backend, err := secretSet(provider.ID, oldRef, "old-secret")
	if err != nil || backend != "file" {
		t.Fatalf("legacy secret setup backend=%q err=%v", backend, err)
	}
	oldRecord := KeyRecord{ID: "old", Name: "old", Plan: "custom", Region: "cn", Backend: "file", Ref: oldRef}
	if err := saveProviderKeys(provider.ID, []KeyRecord{oldRecord}); err != nil {
		t.Fatal(err)
	}
	legacyProvider := filepath.Join(legacy, "providers", provider.ID, "provider.json")
	legacyKeys := filepath.Join(legacy, "providers", provider.ID, "keys.json")
	legacySecrets := filepath.Join(legacy, "providers", provider.ID, "secrets.json")
	legacyProviderBefore, _ := os.ReadFile(legacyProvider)
	legacyKeysBefore, _ := os.ReadFile(legacyKeys)
	legacySecretsBefore, _ := os.ReadFile(legacySecrets)

	if err := os.MkdirAll(current, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := configDir(); got != current {
		t.Fatalf("current config did not become the write target: %q", got)
	}
	if resolved, ok := buildIndex()[provider.Alias]; !ok || resolved.Prov.ID != provider.ID {
		t.Fatalf("legacy provider.json did not fall back: %#v", resolved.Prov)
	}
	if keys, err := loadProviderKeys(provider.ID); err != nil || len(keys) != 1 || keys[0].ID != "old" {
		t.Fatalf("legacy keys fallback = %#v, err=%v", keys, err)
	}
	if value, err := secretGet(provider.ID, "file", oldRef); err != nil || value != "old-secret" {
		t.Fatalf("legacy secret fallback = %q, err=%v", value, err)
	}

	newRef := "provider/custom-migrate/key/new"
	if _, err := secretSet(provider.ID, newRef, "new-secret"); err != nil {
		t.Fatal(err)
	}
	newRecord := KeyRecord{ID: "new", Name: "new", Plan: "custom", Region: "cn", Backend: "file", Ref: newRef}
	if err := saveProviderKeys(provider.ID, []KeyRecord{oldRecord, newRecord}); err != nil {
		t.Fatal(err)
	}
	provider.Name = "Current"
	if err := atomicWriteJSON(customProviderPath(provider.ID), customProviderFile{Version: 1, Provider: provider}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"provider.json", "keys.json", "secrets.json"} {
		path := filepath.Join(current, "providers", provider.ID, name)
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("current %s missing or not private: mode=%v err=%v", name, info, err)
		}
	}
	for ref, want := range map[string]string{oldRef: "old-secret", newRef: "new-secret"} {
		if value, err := secretGet(provider.ID, "file", ref); err != nil || value != want {
			t.Fatalf("migrated secret %s = %q, err=%v", ref, value, err)
		}
	}
	if err := deleteKeyRecord(provider.ID, "old"); err != nil {
		t.Fatal(err)
	}
	if keys, err := loadProviderKeys(provider.ID); err != nil || len(keys) != 1 || keys[0].ID != "new" {
		t.Fatalf("post-delete keys = %#v, err=%v", keys, err)
	}
	if _, err := secretGet(provider.ID, "file", oldRef); err == nil {
		t.Fatal("deleted migrated secret remained active")
	}
	if value, err := secretGet(provider.ID, "file", newRef); err != nil || value != "new-secret" {
		t.Fatalf("unrelated migrated secret = %q, err=%v", value, err)
	}
	for path, before := range map[string][]byte{
		legacyProvider: legacyProviderBefore,
		legacyKeys:     legacyKeysBefore,
		legacySecrets:  legacySecretsBefore,
	} {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("legacy source changed during migration: %s err=%v", path, err)
		}
	}
}

func TestSecretSetDoesNotShadowOversizedLegacyStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("MUXLM_CONFIG_DIR", "")
	t.Setenv("PROVIDERDECK_CONFIG_DIR", "")
	t.Setenv("CX_CONFIG_DIR", "")
	t.Setenv("MUXLM_SECRET_BACKEND", "file")
	legacy := filepath.Join(home, ".config", "providerdeck")
	current := filepath.Join(home, ".config", "muxlm")
	legacyPath := filepath.Join(legacy, "providers", "minimax", "secrets.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	oversized := bytes.Repeat([]byte{'x'}, maxPrivateFileBytes+1)
	if err := os.WriteFile(legacyPath, oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(current, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := secretSet("minimax", "provider/minimax/key/new", "new-secret"); err == nil || !strings.Contains(err.Error(), "2 MiB") {
		t.Fatalf("non-ENOENT legacy read error was ignored: %v", err)
	}
	primaryPath := filepath.Join(current, "providers", "minimax", "secrets.json")
	if _, err := os.Lstat(primaryPath); !os.IsNotExist(err) {
		t.Fatalf("failed legacy read created a shadow store: %v", err)
	}
	if info, err := os.Stat(legacyPath); err != nil || info.Size() != int64(len(oversized)) {
		t.Fatalf("legacy secret store was changed: info=%v err=%v", info, err)
	}
}

func TestDefaultSourcesUseGitHubMuxLM(t *testing.T) {
	if defaultCatalogURL != "https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/catalog.json" {
		t.Fatalf("catalog source = %q", defaultCatalogURL)
	}
	if defaultReleaseAPIURL != "https://api.github.com/repos/Neo-Isshin/MuxLM/releases/latest" {
		t.Fatalf("release source = %q", defaultReleaseAPIURL)
	}
	if defaultInstallURL != "https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh" {
		t.Fatalf("installer source = %q", defaultInstallURL)
	}
}

func TestCatalogAcceptsCanonicalAndLegacyProviderKeyNamespaces(t *testing.T) {
	for _, keyEnv := range []string{"MUXLM_PROVIDER_TESTDECK_KEY", "PROVIDERDECK_PROVIDER_TESTDECK_KEY", "CX_PROVIDER_TESTDECK_KEY"} {
		catalog := cloneCatalog(t, &embeddedCatalog)
		catalog.Providers = append(catalog.Providers, Provider{
			ID:        "testdeck",
			Alias:     "testdeck",
			Name:      "Test Deck",
			OpenAIURL: "https://example.com",
			KeyEnv:    keyEnv,
			CLI:       []string{"codex"},
			Models:    []Model{{ID: "test-model", Latest: true}},
		})
		if err := validateCatalog(catalog); err != nil {
			t.Fatalf("key namespace %s was rejected: %v", keyEnv, err)
		}
	}
}

func TestChildEnvScrubsAllMuxLMKeyNamespaces(t *testing.T) {
	isolatedConfig(t)
	t.Setenv("MUXLM_PROVIDER_RETIRED_KEY", "muxlm-retired-secret")
	t.Setenv("MUXLM_CUSTOM_KEY", "muxlm-custom-secret")
	t.Setenv("PROVIDERDECK_PROVIDER_RETIRED_KEY", "retired-secret")
	t.Setenv("PROVIDERDECK_CUSTOM_KEY", "custom-secret")
	t.Setenv("CX_PROVIDER_RETIRED_KEY", "cx-retired-secret")
	t.Setenv("CX_CUSTOM_KEY", "cx-custom-secret")
	env := strings.Join(childEnv(nil), "\n")
	for _, secret := range []string{"muxlm-retired-secret", "muxlm-custom-secret", "retired-secret", "custom-secret", "cx-retired-secret", "cx-custom-secret"} {
		if strings.Contains(env, secret) {
			t.Fatalf("key leaked to child process: %s", secret)
		}
	}
}

func TestSecretBackendWritesMuxLMServiceAndReadsLegacyServices(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is only used on supported Unix platforms")
	}
	root := t.TempDir()
	script := `#!/bin/sh
case "$1" in
  add-generic-password)
    printf '%s' "$*" > "$CAPTURE"
    cat >/dev/null
    ;;
  find-generic-password)
    case " $* " in
      *" -s $LEGACY_SERVICE "*) printf '%s' "$LEGACY_SERVICE-secret" ;;
      *) exit 44 ;;
    esac
    ;;
  *) exit 45 ;;
esac
`
	security := filepath.Join(root, "security")
	if err := os.WriteFile(security, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(root, "args")
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE", capture)
	t.Setenv("MUXLM_SECRET_BACKEND", "keychain")
	if _, err := secretSet("test", "provider/test/key/main", "new-secret"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(args), "-s muxlm") || strings.Contains(string(args), "new-secret") {
		t.Fatalf("new service args = %q, err=%v", args, err)
	}
	if got := secretServicesForRead(); strings.Join(got, ",") != "muxlm,providerdeck,ez-switch" {
		t.Fatalf("secret service precedence = %#v", got)
	}
	for _, service := range []string{"providerdeck", "ez-switch"} {
		t.Setenv("LEGACY_SERVICE", service)
		got, err := secretGet("test", "keychain", "provider/test/key/main")
		if err != nil || got != service+"-secret" {
			t.Fatalf("%s fallback = %q, err=%v", service, got, err)
		}
	}
}

func TestGeneratedConfigsUseMuxLMIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is only used on supported Unix platforms")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(root, "config.toml")
	script := "#!/bin/sh\ncp \"$CODEX_HOME/config.toml\" \"$CAPTURE\"\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE", capture)
	provider := Provider{ID: "test", Alias: "test", Name: "Test", OpenAIURL: "https://example.com", Key: "secret", CLI: []string{"codex", "opencode"}}
	if err := launchCodex(&provider, "model", false, false, nil); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(config), `model_provider = "muxlm"`) || !strings.Contains(string(config), `[model_providers.muxlm]`) {
		t.Fatalf("Codex config = %q, err=%v", config, err)
	}
	previewOutput := captureStdout(t, func() { preview("opencode", &provider, "model", false, false, nil) })
	if !strings.Contains(previewOutput, "muxlm/model") {
		t.Fatalf("OpenCode preview does not use MuxLM identity: %s", previewOutput)
	}
}

func TestReleaseAssetNamingMatchesInstaller(t *testing.T) {
	installer, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := os.ReadFile(filepath.Join(".github", "workflows", "ci-release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installer), `ASSET="muxlm-$GOOS-$GOARCH"`) || !strings.Contains(string(installer), `LEGACY_ASSET="providerdeck-$GOOS-$GOARCH"`) {
		t.Fatal("installer asset name is not canonical")
	}
	if !strings.Contains(string(workflow), `dist/muxlm-$os-$arch`) || !strings.Contains(string(workflow), `dist/providerdeck-$os-$arch`) || !strings.Contains(string(workflow), `sha256sum muxlm-* providerdeck-* > SHA256SUMS`) {
		t.Fatal("release workflow does not publish canonical and compatibility assets")
	}
}

func TestVersionOutputUsesMuxLMBrand(t *testing.T) {
	isolatedConfig(t)
	output := captureStdout(t, printVersion)
	if !strings.HasPrefix(output, "MuxLM v2.2.0\n") {
		t.Fatalf("version output = %q", output)
	}
}
