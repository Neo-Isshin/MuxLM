package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func isolatedConfig(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("CX_CONFIG_DIR", filepath.Join(d, "config"))
	t.Setenv("CX_SECRET_BACKEND", "file")
	return d
}

func TestModelAliasesShareProviderKeys(t *testing.T) {
	isolatedConfig(t)
	idx := buildIndex()
	for _, alias := range []string{"m", "m3", "m27"} {
		if idx[alias].Prov.providerID() != "minimax" {
			t.Fatalf("%s provider id = %q", alias, idx[alias].Prov.providerID())
		}
	}
	backend, err := secretSet("minimax", "provider/minimax/key/test", "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	rec := KeyRecord{ID: "test", Name: "key1", Plan: "standard", Region: "cn", Backend: backend, Ref: "provider/minimax/key/test"}
	if err := saveProviderKeys("minimax", []KeyRecord{rec}); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"m3", "m27"} {
		cs, err := keyCandidates(idx[alias].Prov, "cn")
		if err != nil || len(cs) != 1 || cs[0].Name != "key1" {
			t.Fatalf("%s candidates = %#v, %v", alias, cs, err)
		}
	}
}

func TestPlansShareDirectoryButKeepSeparateKeys(t *testing.T) {
	isolatedConfig(t)
	idx := buildIndex()
	if idx["glm"].Prov.providerID() != idx["glmc"].Prov.providerID() {
		t.Fatal("glm plans must share provider directory")
	}
	keys := []KeyRecord{
		{ID: "payg", Name: "payg", Plan: "standard", Region: "cn", Backend: "file", Ref: "provider/glm/key/payg"},
		{ID: "coding", Name: "coding", Plan: "coding", Region: "cn", Backend: "file", Ref: "provider/glm/key/coding"},
	}
	if err := saveProviderKeys("glm", keys); err != nil {
		t.Fatal(err)
	}
	for id := range map[string]string{"payg": "one", "coding": "two"} {
		ref := "provider/glm/key/" + id
		if _, err := secretSet("glm", ref, map[string]string{"payg": "one", "coding": "two"}[id]); err != nil {
			t.Fatal(err)
		}
	}
	standard, _ := keyCandidates(idx["glm"].Prov, "cn")
	coding, _ := keyCandidates(idx["glmc"].Prov, "cn")
	if len(standard) != 1 || standard[0].Name != "payg" {
		t.Fatalf("standard=%#v", standard)
	}
	if len(coding) != 1 || coding[0].Name != "coding" {
		t.Fatalf("coding=%#v", coding)
	}
}

func TestLegacyDuplicateLastWins(t *testing.T) {
	isolatedConfig(t)
	if err := ensurePrivateDir(configDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keysFile(), []byte("MINIMAX_KEY=old\nMINIMAX_KEY=new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadLegacyKeys()["MINIMAX_KEY"]; got != "new" {
		t.Fatalf("got %q", got)
	}
}

func TestAtomicWritePermissionsAndSymlinkRefusal(t *testing.T) {
	d := isolatedConfig(t)
	path := filepath.Join(configDir(), "test.json")
	if err := atomicWriteJSON(path, map[string]string{"ok": "yes"}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if got := mustStat(t, path).Mode().Perm(); got != 0o600 {
			t.Fatalf("mode=%o", got)
		}
		link := filepath.Join(configDir(), "link.json")
		if err := os.Symlink(filepath.Join(d, "target"), link); err != nil {
			t.Fatal(err)
		}
		if err := atomicWriteJSON(link, map[string]string{"x": "y"}); err == nil {
			t.Fatal("expected symlink refusal")
		}
	}
}

func TestKeychainPasswordInputConfirmsWithoutArgvSecret(t *testing.T) {
	if got := keychainPasswordInput("secret"); got != "secret\nsecret\n" {
		t.Fatalf("unexpected keychain stdin: %q", got)
	}
}

func TestLiveMacOSKeychainBackend(t *testing.T) {
	if os.Getenv("CX_LIVE_KEYCHAIN_TEST") != "1" || runtime.GOOS != "darwin" {
		t.Skip("set CX_LIVE_KEYCHAIN_TEST=1 for reversible macOS Keychain smoke")
	}
	t.Setenv("CX_SECRET_BACKEND", "keychain")
	ref := "provider/audit/key/" + randomID()
	backend, err := secretSet("audit", ref, "audit-dummy-value")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = secretDelete("audit", backend, ref) }()
	got, err := secretGet("audit", backend, ref)
	if err != nil || got != "audit-dummy-value" {
		t.Fatalf("get=%q err=%v", got, err)
	}
	if err := secretDelete("audit", backend, ref); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointSecurity(t *testing.T) {
	if err := validateEndpoint("http://api.example.com", false); err == nil {
		t.Fatal("public HTTP should be rejected")
	}
	if err := validateEndpoint("http://localhost:8080", false); err != nil {
		t.Fatal(err)
	}
	if err := validateEndpoint("https://user:pass@example.com", false); err == nil {
		t.Fatal("URL credentials should be rejected")
	}
}

func TestProviderIDCannotEscapeDirectory(t *testing.T) {
	for _, id := range []string{".", "..", ".hidden"} {
		if safeID(id) != "" {
			t.Fatalf("unsafe id %q accepted", id)
		}
	}
}

func TestTamperedKeyMetadataIsRejected(t *testing.T) {
	isolatedConfig(t)
	bad := []KeyRecord{{ID: "key1", Name: "key1", Plan: "standard", Region: "cn", Backend: "keychain", Ref: "--malicious-option"}}
	if err := atomicWriteJSON(providerKeysFile("minimax"), keyFile{Version: 1, Keys: bad}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProviderKeys("minimax"); err == nil {
		t.Fatal("tampered secret_ref was accepted")
	}
}

func TestTamperedCustomProviderIsIgnored(t *testing.T) {
	isolatedConfig(t)
	p := Provider{ID: "../escape", Alias: "escape", Plan: "custom", OpenAIURL: "http://169.254.169.254", KeyEnv: "BAD", CLI: []string{"codex"}, Models: []Model{{ID: "x", Latest: true}}}
	if err := atomicWriteJSON(customProviderPath("custom-escape"), customProviderFile{Version: 1, Provider: p}); err != nil {
		t.Fatal(err)
	}
	if _, ok := buildIndex()["escape"]; ok {
		t.Fatal("tampered custom provider was loaded")
	}
}

func TestLaunchCodexAlwaysRemovesSecretTempDir(t *testing.T) {
	root := isolatedConfig(t)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(root, "capture")
	script := "#!/bin/sh\nprintf '%s' \"$CODEX_HOME\" > \"$CAPTURE\"\nexit \"${FAKE_EXIT:-0}\"\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE", capture)
	t.Setenv("MINIMAX_KEY", "test-secret")
	p := buildIndex()["m3"].Prov
	if err := launchCodex(p, "MiniMax-M3", false, false, nil); err != nil {
		t.Fatal(err)
	}
	assertCapturedDirRemoved(t, capture)
	t.Setenv("FAKE_EXIT", "7")
	err := launchCodex(p, "MiniMax-M3", false, false, nil)
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 7 {
		t.Fatalf("err=%v", err)
	}
	assertCapturedDirRemoved(t, capture)
}

func TestLaunchClaudeAndOpencode(t *testing.T) {
	root := isolatedConfig(t)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	captureClaude := filepath.Join(root, "claude-env")
	claudeScript := "#!/bin/sh\nprintf '%s|%s' \"$ANTHROPIC_BASE_URL\" \"$ANTHROPIC_AUTH_TOKEN\" > \"$CAPTURE_CLAUDE\"\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(claudeScript), 0o700); err != nil {
		t.Fatal(err)
	}
	captureOpen := filepath.Join(root, "opencode-dir")
	captureOpenArgs := filepath.Join(root, "opencode-args")
	openScript := "#!/bin/sh\ntest -f \"$OPENCODE_CONFIG_DIR/opencode.json\" || exit 9\ngrep -q '\"permission\": \"allow\"' \"$OPENCODE_CONFIG_DIR/opencode.json\" || exit 10\nprintf '%s' \"$OPENCODE_CONFIG_DIR\" > \"$CAPTURE_OPEN\"\nprintf '%s' \"$*\" > \"$CAPTURE_OPEN_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(openScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE_CLAUDE", captureClaude)
	t.Setenv("CAPTURE_OPEN", captureOpen)
	t.Setenv("CAPTURE_OPEN_ARGS", captureOpenArgs)
	p := Provider{ID: "test", Alias: "test", Name: "Test", Plan: "custom", ClaudeURL: "https://example.com", Key: "chosen-secret", KeyEnv: "TEST_KEY", CLI: []string{"claude", "opencode"}, Models: []Model{{ID: "model", Latest: true}}}
	if err := launchClaude(&p, "model", false, false, nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(captureClaude)
	if err != nil || string(b) != "https://example.com|chosen-secret" {
		t.Fatalf("claude env=%q err=%v", b, err)
	}
	if err := launchOpencode(&p, "model", true, false, nil); err != nil {
		t.Fatal(err)
	}
	assertCapturedDirRemoved(t, captureOpen)
	args, err := os.ReadFile(captureOpenArgs)
	if err != nil || !strings.Contains(string(args), "--auto") || strings.Contains(string(args), "--force") {
		t.Fatalf("opencode args=%q err=%v", args, err)
	}
}

func TestChildEnvScrubsOtherProviderKeys(t *testing.T) {
	isolatedConfig(t)
	t.Setenv("GLM_KEY", "glm-secret")
	t.Setenv("MINIMAX_KEY", "minimax-secret")
	env := strings.Join(childEnv(map[string]string{"ANTHROPIC_AUTH_TOKEN": "chosen"}), "\n")
	if strings.Contains(env, "glm-secret") || strings.Contains(env, "minimax-secret") {
		t.Fatal("unrelated provider key leaked to child")
	}
	if !strings.Contains(env, "ANTHROPIC_AUTH_TOKEN=chosen") {
		t.Fatal("chosen key missing")
	}
}

func TestConfigNeverPrintsSecretValues(t *testing.T) {
	isolatedConfig(t)
	t.Setenv("GLM_KEY", "do-not-print-this-secret")
	output := captureStdout(t, func() {
		if err := printConfig("claude"); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(output, "do-not-print-this-secret") {
		t.Fatal("config leaked secret value")
	}
	if !strings.Contains(output, "+env") {
		t.Fatal("config did not report environment source")
	}
}

func TestCatalogUpdateValidatesAndWrites(t *testing.T) {
	isolatedConfig(t)
	b, err := os.ReadFile("catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(b) }))
	defer srv.Close()
	t.Setenv("CX_CATALOG_URL", srv.URL)
	if err := runCatalogUpdate(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(catalogCacheFile()); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogUpdateRejectsUnknownSecretField(t *testing.T) {
	isolatedConfig(t)
	body := `{"version":1,"revision":"bad","secret":"must-not-be-accepted","providers":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()
	t.Setenv("CX_CATALOG_URL", srv.URL)
	if err := runCatalogUpdate(); err == nil {
		t.Fatal("catalog with unknown secret field was accepted")
	}
	if _, err := os.Stat(catalogCacheFile()); !os.IsNotExist(err) {
		t.Fatalf("invalid catalog was written: %v", err)
	}
}

func TestAddCustomProviderStoresNamedKeySeparately(t *testing.T) {
	isolatedConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	withStdin(t, "mine\n"+srv.URL+"\nmodel-x\nfriendly\nsecret-x\n", func() {
		if err := runAddCustom("claude"); err != nil {
			t.Fatal(err)
		}
	})
	r, ok := buildIndex()["mine"]
	if !ok || r.Prov.Key != "" {
		t.Fatalf("custom provider not loaded safely: %#v", r.Prov)
	}
	keys, err := loadProviderKeys("custom-mine")
	if err != nil || len(keys) != 1 || keys[0].Name != "friendly" {
		t.Fatalf("keys=%#v err=%v", keys, err)
	}
	b, err := os.ReadFile(customProviderPath("custom-mine"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret-x") {
		t.Fatal("provider metadata contains secret")
	}
}

func TestDeleteFromMultiKeyChooserRequiresConfirmation(t *testing.T) {
	isolatedConfig(t)
	p := buildIndex()["m3"].Prov
	var records []KeyRecord
	for i, name := range []string{"key1", "key2"} {
		id := name
		ref := "provider/minimax/key/" + id
		backend, err := secretSet("minimax", ref, "secret"+string(rune('1'+i)))
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, KeyRecord{ID: id, Name: name, Plan: "standard", Region: "cn", Backend: backend, Ref: ref})
	}
	if err := saveProviderKeys("minimax", records); err != nil {
		t.Fatal(err)
	}
	cs, _ := keyCandidates(p, "cn")
	withStdin(t, "x\n2\nyes\n", func() {
		_, retry, err := chooseKeyCandidate(p, "cn", cs)
		if err != nil || !retry {
			t.Fatalf("retry=%v err=%v", retry, err)
		}
	})
	left, _ := loadProviderKeys("minimax")
	if len(left) != 1 || left[0].Name != "key1" {
		t.Fatalf("left=%#v", left)
	}
}

func TestMultiKeyChooserSelectsRequestedKey(t *testing.T) {
	p := &Provider{ID: "test", Alias: "test", Name: "Test"}
	cs := []keyCandidate{{Name: "key1", Source: "env", Value: "one"}, {Name: "key2", Source: "env", Value: "two"}}
	withStdin(t, "2\n", func() {
		chosen, retry, err := chooseKeyCandidate(p, "cn", cs)
		if err != nil || retry || chosen.Value != "two" {
			t.Fatalf("chosen=%#v retry=%v err=%v", chosen, retry, err)
		}
	})
}

func TestSetKeyAndRemoveCustomProvider(t *testing.T) {
	isolatedConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	p := Provider{ID: "custom-mine", Alias: "mine", Name: "Mine", Plan: "custom", ClaudeURL: srv.URL, KeyEnv: "CX_MINE_KEY", CLI: []string{"claude", "opencode"}, Models: []Model{{ID: "model", Latest: true}}}
	if err := atomicWriteJSON(customProviderPath(p.ID), customProviderFile{Version: 1, Provider: p}); err != nil {
		t.Fatal(err)
	}
	withStdin(t, "primary\nsecret\n", func() {
		if err := runSetKey("claude", "mine"); err != nil {
			t.Fatal(err)
		}
	})
	keys, err := loadProviderKeys(p.ID)
	if err != nil || len(keys) != 1 || keys[0].Name != "primary" {
		t.Fatalf("keys=%#v err=%v", keys, err)
	}
	withStdin(t, "yes\n", func() {
		if err := runRemove("mine"); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := os.Stat(providerDir(p.ID)); !os.IsNotExist(err) {
		t.Fatalf("provider directory remains: %v", err)
	}
}

func TestRunAddCustomChoiceAndRendering(t *testing.T) {
	isolatedConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	choices := 0
	for _, p := range catalogProviders() {
		if p.supports("claude") {
			choices++
		}
	}
	input := fmt.Sprintf("%d\nadded\n%s\nmodel\nkey1\nsecret\n", choices+1, srv.URL)
	withStdin(t, input, func() {
		if err := runAdd("claude"); err != nil {
			t.Fatal(err)
		}
	})
	if _, ok := buildIndex()["added"]; !ok {
		t.Fatal("runAdd did not add custom provider")
	}
	out := captureStdout(t, func() {
		printTable()
		preview("claude", buildIndex()["added"].Prov, "model", false, false, nil)
	})
	if !strings.Contains(out, "added") || strings.Contains(out, "secret") {
		t.Fatalf("unsafe or incomplete rendering: %s", out)
	}
}

func TestPromptProtocolAndStatusMessages(t *testing.T) {
	withStdin(t, "2\n", func() {
		if got := promptProtocol(); got != "anthropic" {
			t.Fatalf("got %s", got)
		}
	})
	if !strings.Contains(statusMsg(true, 401), "key") {
		t.Fatal("401 status message missing key hint")
	}
	if !strings.Contains(statusMsg(true, 429), "限流") {
		t.Fatal("429 status message missing rate-limit hint")
	}
	if !strings.Contains(statusMsg(false, 0), "连不上") {
		t.Fatal("network status message missing")
	}
}

func TestHelpDetectionStopsAtPassthroughSeparator(t *testing.T) {
	if !hasAny([]string{"m3", "--help"}, "-h", "--help", "help") {
		t.Fatal("tool help before separator not detected")
	}
	if hasAny([]string{"m3", "--", "--help"}, "-h", "--help", "help") {
		t.Fatal("passthrough help was intercepted")
	}
}

func assertCapturedDirRemoved(t *testing.T, capture string) {
	t.Helper()
	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(b)); !os.IsNotExist(err) {
		t.Fatalf("temporary secret directory still exists: %s", b)
	}
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() { os.Stdin = old; _ = r.Close() }()
	fn()
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
