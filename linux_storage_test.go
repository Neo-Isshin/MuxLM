package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func clearConfigOverrides(t *testing.T) {
	t.Helper()
	t.Setenv("MUXLM_CONFIG_DIR", "")
	t.Setenv("PROVIDERDECK_CONFIG_DIR", "")
	t.Setenv("CX_CONFIG_DIR", "")
}

func clearSecretBackendOverrides(t *testing.T) {
	t.Helper()
	t.Setenv("MUXLM_SECRET_BACKEND", "")
	t.Setenv("PROVIDERDECK_SECRET_BACKEND", "")
	t.Setenv("CX_SECRET_BACKEND", "")
}

func TestLinuxXDGConfigRootKeepsLegacyTreesReadable(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	clearConfigOverrides(t)

	legacyMuxLM := filepath.Join(home, ".config", "muxlm")
	legacyProviderDeck := filepath.Join(home, ".config", "providerdeck")
	legacyCX := filepath.Join(home, ".config", "cx")
	for _, root := range []string{legacyMuxLM, legacyProviderDeck, legacyCX} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	legacyPath := filepath.Join(legacyMuxLM, "keys.env")
	legacyData := []byte("MINIMAX_KEY=legacy\n")
	if err := os.WriteFile(legacyPath, legacyData, 0o600); err != nil {
		t.Fatal(err)
	}

	roots, err := defaultConfigRootsForReadE("linux")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(xdg, "muxlm"),
		legacyMuxLM,
		legacyProviderDeck,
		legacyCX,
	}
	if strings.Join(roots, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Linux XDG roots:\n%q\nwant:\n%q", roots, want)
	}
	data, err := readPrivateFileWithRoots(filepath.Join(roots[0], "keys.env"), roots)
	if err != nil || string(data) != string(legacyData) {
		t.Fatalf("legacy ~/.config/muxlm read = %q, err=%v", data, err)
	}

	if err := os.MkdirAll(roots[0], 0o700); err != nil {
		t.Fatal(err)
	}
	xdgData := []byte("MINIMAX_KEY=xdg\n")
	if err := os.WriteFile(filepath.Join(roots[0], "keys.env"), xdgData, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err = readPrivateFileWithRoots(filepath.Join(roots[0], "keys.env"), roots)
	if err != nil || string(data) != string(xdgData) {
		t.Fatalf("XDG file did not take precedence: %q, err=%v", data, err)
	}
	after, err := os.ReadFile(legacyPath)
	if err != nil || string(after) != string(legacyData) {
		t.Fatalf("legacy source changed: %q, err=%v", after, err)
	}
}

func TestXDGConfigHomeIsLinuxOnlyAndMustBeAbsolute(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	clearConfigOverrides(t)
	legacy := filepath.Join(home, ".config", "providerdeck")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", "relative-config")
	roots, err := defaultConfigRootsForReadE("linux")
	if err != nil || len(roots) == 0 || roots[0] != legacy {
		t.Fatalf("relative XDG root was not ignored: roots=%q err=%v", roots, err)
	}

	absoluteXDG := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", absoluteXDG)
	roots, err = defaultConfigRootsForReadE("darwin")
	if err != nil || len(roots) == 0 || roots[0] != legacy {
		t.Fatalf("XDG changed macOS config selection: roots=%q err=%v", roots, err)
	}
}

func TestAbsoluteLinuxXDGWorksWithoutHome(t *testing.T) {
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	clearConfigOverrides(t)
	roots, err := defaultConfigRootsForReadE("linux")
	if err != nil || len(roots) != 1 || roots[0] != filepath.Join(xdg, "muxlm") {
		t.Fatalf("XDG without HOME: roots=%q err=%v", roots, err)
	}
}

func TestDetectSecretBackendRequiresUsableLinuxSession(t *testing.T) {
	found := func(string) (string, error) { return "/usr/bin/secret-tool", nil }
	missing := func(string) (string, error) { return "", exec.ErrNotFound }
	noEnv := func(string) string { return "" }
	probes := 0
	readyProbe := func() (bool, string) {
		probes++
		return true, ""
	}

	if got := detectSecretBackend("linux", missing, noEnv, readyProbe); got.name != "file" || probes != 0 {
		t.Fatalf("missing secret-tool choice=%#v probes=%d", got, probes)
	}
	if got := detectSecretBackend("linux", found, noEnv, readyProbe); got.name != "file" || !strings.Contains(got.reason, "D-Bus") || probes != 0 {
		t.Fatalf("headless choice=%#v probes=%d", got, probes)
	}
	withBus := func(name string) string {
		if name == "DBUS_SESSION_BUS_ADDRESS" {
			return "unix:path=/run/user/1000/bus"
		}
		return ""
	}
	if got := detectSecretBackend("linux", found, withBus, readyProbe); got.name != "secret-service" || probes != 1 {
		t.Fatalf("ready Secret Service choice=%#v probes=%d", got, probes)
	}
	failedProbe := func() (bool, string) { return false, "Secret Service 不可用：no service" }
	if got := detectSecretBackend("linux", found, withBus, failedProbe); got.name != "file" || !strings.Contains(got.reason, "no service") {
		t.Fatalf("failed Secret Service probe choice=%#v", got)
	}
}

func TestXDGSessionBusIsVisibleWithoutDBusEnvironment(t *testing.T) {
	runtimeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(runtimeDir, "bus"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	getenv := func(name string) string {
		if name == "XDG_RUNTIME_DIR" {
			return runtimeDir
		}
		return ""
	}
	if !visibleSessionBus(getenv) {
		t.Fatal("existing XDG_RUNTIME_DIR/bus was not detected")
	}
	getenv = func(name string) string {
		if name == "XDG_RUNTIME_DIR" {
			return "relative-runtime"
		}
		return ""
	}
	if visibleSessionBus(getenv) {
		t.Fatal("relative XDG_RUNTIME_DIR was accepted")
	}
}

func TestPassiveSecretBackendSelectionDoesNotRunSecretTool(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "called")
	script := "#!/bin/sh\n: > \"$MARKER\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(root, "secret-tool"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	t.Setenv("MARKER", marker)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus")
	clearSecretBackendOverrides(t)

	choice := detectSecretBackend("linux", exec.LookPath, os.Getenv, nil)
	if choice.name != "secret-service" {
		t.Fatalf("passive choice = %#v", choice)
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("passive backend selection executed secret-tool: %v", err)
	}
}

func TestLinuxFileSecretFallbackRequiresConsentAndStaysPrivate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MUXLM_CONFIG_DIR", filepath.Join(root, "config"))
	clearSecretBackendOverrides(t)
	choice := secretBackendChoice{name: "file", reason: "当前会话没有可用的 D-Bus"}
	ref := "provider/minimax/key/linux"

	if _, err := secretSetWithChoice("minimax", ref, "secret", choice, "linux"); err == nil ||
		!strings.Contains(err.Error(), "MUXLM_SECRET_BACKEND=file") {
		t.Fatalf("unconfirmed file fallback error = %v", err)
	}
	if _, err := os.Lstat(fileSecretsPath("minimax")); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed fallback wrote a secret: %v", err)
	}

	choice.explicit = true
	if backend, err := secretSetWithChoice("minimax", ref, "secret", choice, "linux"); err != nil || backend != "file" {
		t.Fatalf("confirmed file fallback backend=%q err=%v", backend, err)
	}
	info, err := os.Stat(fileSecretsPath("minimax"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file fallback mode=%v err=%v", info, err)
	}
}

func TestSecretServiceWriteFailureDoesNotDowngradeToFile(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("shell fixture requires Unix")
	}
	root := t.TempDir()
	config := filepath.Join(root, "config")
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "secret-tool"), []byte("#!/bin/sh\nexit 9\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("MUXLM_CONFIG_DIR", config)

	_, err := secretSetWithBackend("minimax", "provider/minimax/key/fail", "secret", "secret-service")
	if err == nil || !strings.Contains(err.Error(), "MUXLM_SECRET_BACKEND=file") {
		t.Fatalf("Secret Service failure = %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(config, "providers", "minimax", "secrets.json")); !os.IsNotExist(statErr) {
		t.Fatalf("Secret Service failure silently wrote a file: %v", statErr)
	}
}

func TestProbeSecretServiceTreatsMissingPasswordAsReachable(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("shell fixture requires Unix")
	}
	root := t.TempDir()
	path := filepath.Join(root, "secret-tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	if ok, reason := probeSecretService(); !ok || reason != "" {
		t.Fatalf("empty lookup miss was not treated as reachable: ok=%v reason=%q", ok, reason)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho unavailable >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if ok, reason := probeSecretService(); ok || !strings.Contains(reason, "unavailable") {
		t.Fatalf("probe error was accepted: ok=%v reason=%q", ok, reason)
	}
}

func TestReadPrivateFileWithRootsRejectsEmptyRootList(t *testing.T) {
	if _, err := readPrivateFileWithRoots("anything", nil); err == nil {
		t.Fatalf("empty roots error = %v", err)
	}
}
