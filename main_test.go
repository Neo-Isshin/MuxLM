package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func isolatedConfig(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("MUXLM_CONFIG_DIR", filepath.Join(d, "config"))
	t.Setenv("MUXLM_SECRET_BACKEND", "file")
	return d
}

func TestModelAliasesShareProviderKeys(t *testing.T) {
	isolatedConfig(t)
	idx := buildIndex()
	for _, alias := range []string{"m", "m3", "m27std", "m27"} {
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
	for _, alias := range []string{"m3", "m27std", "m27"} {
		cs, err := keyCandidates(idx[alias].Prov, "cn")
		if err != nil || len(cs) != 1 || cs[0].Name != "key1" {
			t.Fatalf("%s candidates = %#v, %v", alias, cs, err)
		}
	}
}

func TestEveryProviderAliasUsesItsLatestModel(t *testing.T) {
	isolatedConfig(t)
	idx := buildIndex()
	for providerIndex := range embeddedCatalog.Providers {
		provider := &embeddedCatalog.Providers[providerIndex]
		var latest *Model
		for modelIndex := range provider.Models {
			if provider.Models[modelIndex].Latest {
				if latest != nil {
					t.Fatalf("%s has more than one latest model", provider.Alias)
				}
				latest = &provider.Models[modelIndex]
			}
		}
		if latest == nil {
			t.Fatalf("%s has no latest model", provider.Alias)
		}
		resolved, exists := idx[provider.Alias]
		if !exists || resolved.Prov.Alias != provider.Alias || resolved.Model.ID != latest.ID {
			t.Fatalf("%s = %#v, want latest %s", provider.Alias, resolved, latest.ID)
		}
	}

	for alias, modelID := range map[string]string{
		"glm": "glm-5.2",
		"k":   "kimi-k3",
		"m":   "MiniMax-M3",
		"ds":  "deepseek-v4-pro",
		"q":   "qwen3.7-plus",
	} {
		if got := idx[alias].Model.ID; got != modelID {
			t.Fatalf("%s default = %q, want %q", alias, got, modelID)
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

func TestBailianCatalogSeparatesPayAsYouGoAndCodingPlan(t *testing.T) {
	isolatedConfig(t)
	idx := buildIndex()
	payg, paygOK := idx["q"]
	coding, codingOK := idx["qc"]
	if !paygOK || !codingOK {
		t.Fatalf("Bailian aliases missing: q=%v qc=%v", paygOK, codingOK)
	}
	if payg.Prov.providerID() != "bailian" || coding.Prov.providerID() != "bailian" {
		t.Fatalf("Bailian provider ids = %q, %q", payg.Prov.providerID(), coding.Prov.providerID())
	}
	if payg.Prov.planID() != "standard" || coding.Prov.planID() != "coding" {
		t.Fatalf("Bailian plans = %q, %q", payg.Prov.planID(), coding.Prov.planID())
	}
	if payg.Prov.OpenAIURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" ||
		coding.Prov.OpenAIURL != "https://coding.dashscope.aliyuncs.com/v1" {
		t.Fatalf("Bailian OpenAI routes = %q, %q", payg.Prov.OpenAIURL, coding.Prov.OpenAIURL)
	}
	if payg.Prov.ClaudeURL != "https://dashscope.aliyuncs.com/apps/anthropic" ||
		coding.Prov.ClaudeURL != "https://coding.dashscope.aliyuncs.com/apps/anthropic" {
		t.Fatalf("Bailian Anthropic routes = %q, %q", payg.Prov.ClaudeURL, coding.Prov.ClaudeURL)
	}
	if payg.Prov.KeyEnv == coding.Prov.KeyEnv {
		t.Fatal("Bailian pay-as-you-go and Coding Plan must not share a key identity")
	}
	if payg.Model.ID != "qwen3.7-plus" || coding.Model.ID != "qwen3.7-plus" {
		t.Fatalf("Bailian latest models = %q, %q", payg.Model.ID, coding.Model.ID)
	}
	for _, alias := range []string{"q37", "q37m", "qcn", "qcp", "qc37", "qc36", "qck25", "qcglm5", "qcm25", "qc35", "qc3m", "qccn", "qccp", "qcglm47"} {
		if _, ok := idx[alias]; !ok {
			t.Fatalf("Bailian model alias %q missing", alias)
		}
	}
}

func TestOpenRouterCatalogUsesCuratedToolCapableModels(t *testing.T) {
	isolatedConfig(t)
	idx := buildIndex()
	openrouter, ok := idx["or"]
	if !ok {
		t.Fatal("OpenRouter alias missing")
	}
	if openrouter.Prov.OpenAIURL != "https://openrouter.ai/api/v1" ||
		openrouter.Prov.supports("claude") ||
		!openrouter.Prov.supports("codex") ||
		!openrouter.Prov.supports("opencode") {
		t.Fatalf("OpenRouter route = %#v", openrouter.Prov)
	}
	if openrouter.Model.ID != "anthropic/claude-sonnet-5" {
		t.Fatalf("OpenRouter latest model = %q", openrouter.Model.ID)
	}
	for _, alias := range []string{"ors5", "oro48", "ors46", "org56", "orqcn", "orglm52", "ork3", "orm3"} {
		resolved, ok := idx[alias]
		if !ok || resolved.Prov.providerID() != "openrouter" {
			t.Fatalf("OpenRouter model alias %q = %#v", alias, resolved)
		}
	}
}

func TestAliasTableWrapsLongModelListsWithoutHidingAliases(t *testing.T) {
	tags := []string{"qc37", "qc36", "qck25", "qcglm5", "qcm25", "qc35", "qc3m", "qccn", "qccp", "qcglm47"}
	lines := wrapAliasCell("qc", tags, 25)
	if len(lines) < 2 {
		t.Fatalf("long alias list did not wrap: %#v", lines)
	}
	joined := strings.Join(lines, "")
	for _, tag := range tags {
		if !strings.Contains(joined, tag) {
			t.Fatalf("wrapped alias list omitted %q: %#v", tag, lines)
		}
	}
	for _, line := range lines {
		if dispWidth(line) > 25 {
			t.Fatalf("wrapped alias line exceeds column width: %q (%d)", line, dispWidth(line))
		}
	}
}

func TestKimiAliasesSeparateAPIAndCodingPlans(t *testing.T) {
	isolatedConfig(t)
	idx := buildIndex()
	apiModels := map[string]string{
		"k":   "kimi-k3",
		"k27": "kimi-k2.7-code",
		"k26": "kimi-k2.6",
	}
	for alias, model := range apiModels {
		resolved, ok := idx[alias]
		if !ok || resolved.Prov.Alias != "k" || resolved.Prov.Plan != "standard" || resolved.Model.ID != model {
			t.Fatalf("%s = %#v", alias, resolved)
		}
	}

	coding, ok := idx["kc"]
	if !ok || coding.Prov.Plan != "coding" || coding.Model.ID != "kimi-for-coding" || len(coding.Prov.Models) != 1 {
		t.Fatalf("kc = %#v", coding)
	}
	kcModel, kcEnv := claudeLaunchSettings(coding.Prov, coding.Model.ID, coding.Prov.ClaudeURL, "secret")
	if kcModel != "kimi-for-coding" || kcEnv["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != kcModel ||
		kcEnv["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "262144" {
		t.Fatalf("Kimi Coding Claude settings = %q %#v", kcModel, kcEnv)
	}

	for _, retired := range []string{"kimi", "kimic", "kimi26"} {
		if _, exists := idx[retired]; exists {
			t.Fatalf("retired Kimi alias %q is still active", retired)
		}
	}
	k3 := idx["k3"]
	if k3.Prov.Alias != "k" || k3.Model.ID != "kimi-k3" || k3.Model.Source != "official" {
		t.Fatalf("official k3 short name = %#v", k3)
	}

	api := idx["k"].Prov
	if !api.supports("claude") || api.ClaudeURL != "https://api.moonshot.cn/anthropic" {
		t.Fatalf("Kimi API Claude route = %#v", api)
	}
	claudeModel, claudeEnv := claudeLaunchSettings(api, "kimi-k3", api.ClaudeURL, "secret")
	if claudeModel != "kimi-k3[1m]" ||
		claudeEnv["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != claudeModel ||
		claudeEnv["CLAUDE_CODE_SUBAGENT_MODEL"] != claudeModel ||
		claudeEnv["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "1048576" ||
		claudeEnv["ENABLE_TOOL_SEARCH"] != "false" {
		t.Fatalf("Kimi K3 Claude settings = %q %#v", claudeModel, claudeEnv)
	}
	k27Model, k27Env := claudeLaunchSettings(api, "kimi-k2.7-code", api.ClaudeURL, "secret")
	if k27Model != "kimi-k2.7-code" || k27Env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "262144" {
		t.Fatalf("Kimi K2.7 Claude settings = %q %#v", k27Model, k27Env)
	}
}

func TestOfficialShortNamesAndScopedRelayModels(t *testing.T) {
	isolatedConfig(t)
	idx := buildIndex()

	official := idx["k3"]
	if official.Prov.Alias != "k" || official.Model.ID != "kimi-k3" {
		t.Fatalf("k3 did not use Kimi official: %#v", official)
	}
	if minimax := idx["m3"]; minimax.Prov.Alias != "m" || minimax.Model.ID != "MiniMax-M3" {
		t.Fatalf("m3 did not use MiniMax official: %#v", minimax)
	}
	if global := idx["k27"]; global.Prov.Alias != "k" || global.Model.ID != "kimi-k2.7-code" {
		t.Fatalf("k27 did not use Kimi official: %#v", global)
	}

	siliconflow := idx["sf"].Prov
	hosted, ok := resolveProviderModel(siliconflow, "k27")
	if !ok || hosted.Prov.Alias != "sf" || hosted.Model.ID != "moonshotai/Kimi-K2.7-Code" {
		t.Fatalf("sf k27 = %#v, %v", hosted, ok)
	}
	if _, ok := resolveProviderModel(siliconflow, "k3"); ok {
		t.Fatal("sf k3 resolved even though SiliconFlow does not currently publish K3")
	}
	if !knownModelSelector("k3") {
		t.Fatal("official k3 was not recognized as a model selector")
	}
	if !siliconflow.supports("claude") || siliconflow.ClaudeURL != "https://api.siliconflow.cn" {
		t.Fatalf("SiliconFlow Claude route = %#v", siliconflow)
	}
	_, relayEnv := claudeLaunchSettings(siliconflow, hosted.Model.ID, siliconflow.ClaudeURL, "secret")
	if relayEnv["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != hosted.Model.ID ||
		relayEnv["ANTHROPIC_DEFAULT_SONNET_MODEL"] != hosted.Model.ID {
		t.Fatalf("relay Claude model slots = %#v", relayEnv)
	}

	openrouter := idx["or"].Prov
	routed, ok := resolveProviderModel(openrouter, "k3")
	if !ok || routed.Model.ID != "moonshotai/kimi-k3" {
		t.Fatalf("or k3 = %#v, %v", routed, ok)
	}
	relayM3, ok := resolveProviderModel(openrouter, "m3")
	if !ok || relayM3.Model.ID != "minimax/minimax-m3" {
		t.Fatalf("or m3 = %#v, %v", relayM3, ok)
	}
	if got := strings.Join(modelShortcutExamples(siliconflow), ","); !strings.Contains(got, "sf k27") {
		t.Fatalf("SiliconFlow shortcuts = %q", got)
	}
}

func TestLegacyDoubaoKeyPlanMigratesToCodingPlan(t *testing.T) {
	isolatedConfig(t)
	p := buildIndex()["doubao"].Prov
	if p.planID() != "coding" || p.wireAPI() != "responses" || latestModel(p) != "ark-code-latest" {
		t.Fatalf("doubao catalog entry = %#v", p)
	}
	backend, err := secretSet("doubao", "provider/doubao/key/legacy", "legacy-secret")
	if err != nil || backend != "file" {
		t.Fatal(err)
	}
	record := KeyRecord{ID: "legacy", Name: "legacy", Plan: "standard", Region: "cn", Backend: "file", Ref: "provider/doubao/key/legacy"}
	if err := saveProviderKeys("doubao", []KeyRecord{record}); err != nil {
		t.Fatal(err)
	}
	candidates, err := keyCandidates(p, "cn")
	if err != nil || len(candidates) != 1 || candidates[0].Name != "legacy" {
		t.Fatalf("legacy Doubao key candidates = %#v, %v", candidates, err)
	}
	if _, exists := buildIndex()["doubao-code"]; exists {
		t.Fatal("retired Doubao model alias is still active")
	}
	if target := embeddedCatalog.RetiredTags["doubao-code"]; target != "doubao/standard/doubao-seed-code-preview-latest" {
		t.Fatalf("doubao-code tombstone = %q", target)
	}
}

func TestResponsesWireUsesResponsesProbeAndOpenCodeSDK(t *testing.T) {
	var path, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	p := &Provider{OpenAIURL: server.URL, WireAPI: "responses"}
	protocol, base := keyProbeTarget(p, "codex", false)
	if protocol != "responses" || base != server.URL {
		t.Fatalf("probe target = %q %q", protocol, base)
	}
	reachable, code, _ := probe(protocol, base, "ark-code-latest", "secret")
	if !reachable || code != http.StatusOK || path != "/responses" || !strings.Contains(body, `"input":"ping"`) {
		t.Fatalf("responses probe: reachable=%v code=%d path=%q body=%q", reachable, code, path, body)
	}
	if got := openCodeNPM(p); got != "@ai-sdk/openai" {
		t.Fatalf("OpenCode responses SDK = %q", got)
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
		dir := filepath.Join(configDir(), "directory.json")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := atomicWriteJSON(dir, map[string]string{"x": "y"}); err == nil || !strings.Contains(err.Error(), "非普通文件") {
			t.Fatalf("expected directory refusal, got %v", err)
		}
		fifo := filepath.Join(configDir(), "fifo.json")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := atomicWriteJSON(fifo, map[string]string{"x": "y"}); err == nil || !strings.Contains(err.Error(), "非普通文件") {
			t.Fatalf("expected FIFO refusal, got %v", err)
		}
		if info, err := os.Lstat(fifo); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			t.Fatalf("rejected write replaced FIFO: info=%v err=%v", info, err)
		}
	}
}

func TestUpdateLockRejectsNonRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("named pipes use platform-specific creation on Windows")
	}
	isolatedConfig(t)
	if err := ensurePrivateDir(configDir()); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(updateLockFile(), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, acquired, err := tryUpdateLock()
	if lock != nil {
		_ = lock.Close()
	}
	if err == nil || acquired || !strings.Contains(err.Error(), "非普通文件") {
		t.Fatalf("special update lock was accepted: acquired=%v err=%v", acquired, err)
	}
}

func TestReadPrivateFileRejectsSpecialAndOversizedFilesBeforeChmod(t *testing.T) {
	isolatedConfig(t)
	if err := ensurePrivateDir(configDir()); err != nil {
		t.Fatal(err)
	}
	dirPath := filepath.Join(configDir(), "not-a-file.json")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateFile(dirPath); err == nil || !strings.Contains(err.Error(), "非普通文件") {
		t.Fatalf("special file was accepted: %v", err)
	}
	if info, err := os.Stat(dirPath); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("special file was chmodded before rejection: info=%v err=%v", info, err)
	}

	largePath := filepath.Join(configDir(), "too-large.json")
	if err := os.WriteFile(largePath, bytes.Repeat([]byte{'x'}, maxPrivateFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(largePath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateFile(largePath); err == nil || !strings.Contains(err.Error(), "2 MiB") {
		t.Fatalf("oversized private file was accepted: %v", err)
	}
	if info, err := os.Stat(largePath); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("oversized file was chmodded before rejection: info=%v err=%v", info, err)
	}

	guardPath := filepath.Join(configDir(), "write-limit.json")
	if err := atomicWriteJSON(guardPath, map[string]string{"value": "preserve"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(guardPath)
	if err := atomicWriteJSON(guardPath, map[string]string{"value": strings.Repeat("x", maxPrivateFileBytes)}); err == nil || !strings.Contains(err.Error(), "2 MiB") {
		t.Fatalf("oversized atomic write was accepted: %v", err)
	}
	after, _ := os.ReadFile(guardPath)
	if !bytes.Equal(before, after) {
		t.Fatal("rejected oversized write replaced the existing file")
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
	p := buildIndex()["m27std"].Prov
	if err := launchCodex(p, "MiniMax-M2.7", false, false, nil); err != nil {
		t.Fatal(err)
	}
	assertCapturedDirRemoved(t, capture)
	t.Setenv("FAKE_EXIT", "7")
	err := launchCodex(p, "MiniMax-M2.7", false, false, nil)
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

func TestDefaultRouteUsesNativeCLIStateAndScrubsProviderOverrides(t *testing.T) {
	root := isolatedConfig(t)
	bin := filepath.Join(root, "bin")
	captureDir := filepath.Join(root, "capture")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(captureDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
name=${0##*/}
env | sort > "$CAPTURE_DIR/$name.env"
printf '%s' "$*" > "$CAPTURE_DIR/$name.args"
`
	for _, cli := range []string{"claude", "codex", "opencode"} {
		if err := os.WriteFile(filepath.Join(bin, cli), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE_DIR", captureDir)
	t.Setenv("HOME", filepath.Join(root, "native-home"))
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "native-oauth")
	t.Setenv("ANTHROPIC_BASE_URL", "https://relay.example")
	t.Setenv("ANTHROPIC_MODEL", "relay-model")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	t.Setenv("OPENAI_API_KEY", "openai-secret")
	t.Setenv("OPENAI_BASE_URL", "https://openai-relay.example")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "relay-claude"))
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	t.Setenv("CODEX_HOME", filepath.Join(root, "relay-codex"))
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(root, "relay-opencode"))
	t.Setenv("GLM_KEY", "glm-secret")

	for cli, wantArgs := range map[string]string{
		"claude":   "--dangerously-skip-permissions hello world",
		"codex":    "--dangerously-bypass-approvals-and-sandbox hello world",
		"opencode": "--auto hello world",
	} {
		if err := launchDefault(cli, true, []string{"hello", "world"}); err != nil {
			t.Fatalf("%s default launch: %v", cli, err)
		}
		args, err := os.ReadFile(filepath.Join(captureDir, cli+".args"))
		if err != nil || string(args) != wantArgs {
			t.Fatalf("%s default args = %q, %v", cli, args, err)
		}
		env, err := os.ReadFile(filepath.Join(captureDir, cli+".env"))
		if err != nil {
			t.Fatal(err)
		}
		for _, blocked := range []string{
			"ANTHROPIC_BASE_URL=", "ANTHROPIC_MODEL=", "ANTHROPIC_API_KEY=",
			"OPENAI_API_KEY=", "OPENAI_BASE_URL=", "CLAUDE_CONFIG_DIR=",
			"CLAUDE_CODE_USE_BEDROCK=", "CODEX_HOME=", "OPENCODE_CONFIG_DIR=", "GLM_KEY=",
		} {
			if strings.Contains(string(env), blocked) {
				t.Fatalf("%s default launch inherited %s", cli, blocked)
			}
		}
		for _, retained := range []string{"HOME=" + filepath.Join(root, "native-home"), "CLAUDE_CODE_OAUTH_TOKEN=native-oauth"} {
			if !strings.Contains(string(env), retained) {
				t.Fatalf("%s default launch lost native login state %s", cli, retained)
			}
		}
	}

	previewOutput := captureStdout(t, func() {
		previewDefault("claude", false, nil)
		previewDefault("codex", true, []string{"resume"})
	})
	for _, want := range []string{
		"claude → 默认账号 / 默认模型",
		"run  claude\n",
		"run  codex --dangerously-bypass-approvals-and-sandbox resume",
	} {
		if !strings.Contains(previewOutput, want) {
			t.Fatalf("default preview missing %q:\n%s", want, previewOutput)
		}
	}
	if !isReservedAlias("def") || !strings.Contains(helpText, "cld def") {
		t.Fatal("def is not reserved and discoverable")
	}
	listOutput := captureStdout(t, printTable)
	if !strings.Contains(listOutput, "def") || !strings.Contains(listOutput, "原生账号 / 配置") {
		t.Fatalf("list does not show def:\n%s", listOutput)
	}
}

func TestChildEnvScrubsOtherProviderKeys(t *testing.T) {
	isolatedConfig(t)
	t.Setenv("GLM_KEY", "glm-secret")
	t.Setenv("MINIMAX_KEY", "minimax-secret")
	t.Setenv("CX_PROVIDER_RETIRED_KEY", "retired-secret")
	t.Setenv("CX_PROVIDER_RETIRED_KEY_INTL", "retired-intl-secret")
	t.Setenv("ANTHROPIC_MODEL", "stale-model")
	env := strings.Join(childEnv(map[string]string{"ANTHROPIC_AUTH_TOKEN": "chosen", "ANTHROPIC_MODEL": "chosen-model"}), "\n")
	if strings.Contains(env, "glm-secret") || strings.Contains(env, "minimax-secret") || strings.Contains(env, "retired-secret") || strings.Contains(env, "retired-intl-secret") {
		t.Fatal("unrelated provider key leaked to child")
	}
	if !strings.Contains(env, "ANTHROPIC_AUTH_TOKEN=chosen") {
		t.Fatal("chosen key missing")
	}
	if strings.Contains(env, "ANTHROPIC_MODEL=stale-model") || !strings.Contains(env, "ANTHROPIC_MODEL=chosen-model") {
		t.Fatal("selected model environment did not replace stale parent value")
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

func TestConfigViewsShareOneStoreAndFilterByProtocol(t *testing.T) {
	isolatedConfig(t)
	global := captureStdout(t, func() {
		if err := printConfig("claude"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(global, "全局配置中心") || !strings.Contains(global, "ANTHROPIC") || !strings.Contains(global, "OPENAI / WIRE") {
		t.Fatalf("global config header missing: %s", global)
	}
	if !strings.Contains(global, "nvidia") || !strings.Contains(global, "kc") {
		t.Fatalf("global config did not include both protocol-only providers: %s", global)
	}
	openAI := captureStdout(t, func() {
		if err := printConfig("codex"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(openAI, "OpenAI-compatible 过滤视图") || !strings.Contains(openAI, "nvidia") || !strings.Contains(openAI, "kc") {
		t.Fatalf("codex view missing OpenAI providers: %s", openAI)
	}
	opencode := captureStdout(t, func() {
		if err := printConfig("opencode"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(opencode, "kc") || !strings.Contains(opencode, "nvidia") {
		t.Fatalf("opencode view is not dual-protocol: %s", opencode)
	}
}

func TestGlobalConfigSelectsValidationRoute(t *testing.T) {
	both := &Provider{ClaudeURL: "https://anthropic.example", OpenAIURL: "https://openai.example"}
	withStdin(t, "2\n", func() {
		if got := chooseValidationCLI(both); got != "codex" {
			t.Fatalf("got %s", got)
		}
	})
	if got := chooseValidationCLI(&Provider{OpenAIURL: "https://openai.example"}); got != "codex" {
		t.Fatalf("openai-only got %s", got)
	}
	if got := chooseValidationCLI(&Provider{ClaudeURL: "https://anthropic.example"}); got != "claude" {
		t.Fatalf("anthropic-only got %s", got)
	}
}

func TestCldGlobalConfigCanManageOpenAIOnlyProvider(t *testing.T) {
	isolatedConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	p := Provider{ID: "custom-open-only", Alias: "open-only", Name: "Open only", Plan: "custom", OpenAIURL: srv.URL, KeyEnv: "CX_OPEN_ONLY_KEY", CLI: []string{"codex", "opencode"}, Models: []Model{{ID: "model", Latest: true}}}
	if err := atomicWriteJSON(customProviderPath(p.ID), customProviderFile{Version: 1, Provider: p}); err != nil {
		t.Fatal(err)
	}
	withStdin(t, "primary\nsecret\n", func() {
		if err := runSetKeyScoped("claude", "open-only", true); err != nil {
			t.Fatal(err)
		}
	})
	keys, err := loadProviderKeys(p.ID)
	if err != nil || len(keys) != 1 || keys[0].Name != "primary" {
		t.Fatalf("keys=%#v err=%v", keys, err)
	}
}

func TestConfigMenuCanExitWithoutMutation(t *testing.T) {
	isolatedConfig(t)
	withStdin(t, "0\n", func() {
		if err := runConfigMenu("claude"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCdxConfigCannotRemoveAnthropicOnlyRoute(t *testing.T) {
	isolatedConfig(t)
	p := Provider{ID: "custom-anthropic-only", Alias: "anthropic-only", Name: "Anthropic only", Plan: "custom", ClaudeURL: "https://example.com", KeyEnv: "PROVIDERDECK_ANTHROPIC_ONLY_KEY", CLI: []string{"claude", "opencode"}, Models: []Model{{ID: "model", Latest: true}}}
	if err := atomicWriteJSON(customProviderPath(p.ID), customProviderFile{Version: 1, Provider: p}); err != nil {
		t.Fatal(err)
	}
	if err := runRemoveScoped("anthropic-only", "codex", false); err == nil || !strings.Contains(err.Error(), "OpenAI-compatible") {
		t.Fatalf("unexpected error: %v", err)
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

func TestCatalogConditionalRequestUsesETagAnd304(t *testing.T) {
	isolatedConfig(t)
	body := append([]byte(nil), embeddedCatalogJSON...)
	requests := 0
	conditionalHeader := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("ETag", `"catalog-v1"`)
			_, _ = w.Write(body)
			return
		}
		conditionalHeader = r.Header.Get("If-None-Match")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	t.Setenv("CX_CATALOG_URL", srv.URL)
	ctx := context.Background()
	first, err := checkCatalogUpdate(ctx, true)
	if err != nil {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := checkCatalogUpdate(ctx, true)
	if err != nil || !second.NotModified {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	if conditionalHeader != `"catalog-v1"` {
		t.Fatalf("If-None-Match=%q", conditionalHeader)
	}
}

func TestCatalogInvalidCacheNeverUsesValidators(t *testing.T) {
	isolatedConfig(t)
	body := append([]byte(nil), embeddedCatalogJSON...)
	request := 0
	secondConditional := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request++
		if request == 2 {
			secondConditional = r.Header.Get("If-None-Match")
		}
		w.Header().Set("ETag", `"catalog-v1"`)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	t.Setenv("CX_CATALOG_URL", srv.URL)
	if _, err := checkCatalogUpdate(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogCacheFile(), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := checkCatalogUpdate(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if secondConditional != "" {
		t.Fatalf("sent validator for invalid cache: %q", secondConditional)
	}
}

func TestCatalogRejectsRollbackWithoutChangingCache(t *testing.T) {
	isolatedConfig(t)
	future := cloneCatalog(t, &embeddedCatalog)
	future.Revision = "2099-01-01.1"
	if err := atomicWriteJSON(catalogCacheFile(), future); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(catalogCacheFile())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(embeddedCatalogJSON)
	}))
	defer srv.Close()
	t.Setenv("CX_CATALOG_URL", srv.URL)
	if _, err := checkCatalogUpdate(context.Background(), false); err == nil || !strings.Contains(err.Error(), "回滚") {
		t.Fatalf("expected rollback error, got %v", err)
	}
	after, _ := os.ReadFile(catalogCacheFile())
	if !bytes.Equal(before, after) {
		t.Fatal("rollback changed the cached catalog")
	}
}

func TestCatalogRejectsChangedContentWithSameRevision(t *testing.T) {
	isolatedConfig(t)
	body := append([]byte(nil), embeddedCatalogJSON...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	t.Setenv("CX_CATALOG_URL", srv.URL)
	if _, err := checkCatalogUpdate(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	cacheBefore, _ := os.ReadFile(catalogCacheFile())
	stateBefore, _ := os.ReadFile(updateStateFile())
	changed := cloneCatalog(t, &embeddedCatalog)
	changed.Providers[0].Name = "changed without a revision bump"
	body, _ = json.Marshal(changed)
	if _, err := checkCatalogUpdate(context.Background(), false); err == nil || !strings.Contains(err.Error(), "内容") {
		t.Fatalf("expected immutable revision error, got %v", err)
	}
	cacheAfter, _ := os.ReadFile(catalogCacheFile())
	stateAfter, _ := os.ReadFile(updateStateFile())
	if !bytes.Equal(cacheBefore, cacheAfter) || !bytes.Equal(stateBefore, stateAfter) {
		t.Fatal("rejected catalog changed cache or update state")
	}
}

func TestCatalogRejectsUnsolicited304(t *testing.T) {
	isolatedConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	t.Setenv("CX_CATALOG_URL", srv.URL)
	if _, err := checkCatalogUpdate(context.Background(), false); err == nil || !strings.Contains(err.Error(), "304") {
		t.Fatalf("unsolicited 304 accepted: %v", err)
	}
}

func TestCatalogSchemaProtectsSecretNamespaces(t *testing.T) {
	c := cloneCatalog(t, &embeddedCatalog)
	c.Providers = append(c.Providers, Provider{
		ID:        "custom-escape",
		Alias:     "escape",
		Name:      "Escape",
		Plan:      "custom",
		OpenAIURL: "https://example.com",
		KeyEnv:    "AWS_SECRET_ACCESS_KEY",
		CLI:       []string{"codex"},
		Models:    []Model{{ID: "model", Latest: true}},
	})
	if err := validateCatalog(c); err == nil {
		t.Fatal("catalog accepted local custom namespace and unrelated secret env")
	}
	c = cloneCatalog(t, &embeddedCatalog)
	c.Providers = append(c.Providers, Provider{
		ID:        "new-provider",
		Alias:     "newp",
		Name:      "New provider",
		OpenAIURL: "https://example.com",
		KeyEnv:    "AWS_SECRET_ACCESS_KEY",
		CLI:       []string{"codex"},
		Models:    []Model{{ID: "model", Latest: true}},
	})
	if err := validateCatalog(c); err == nil || !strings.Contains(err.Error(), "key_env") {
		t.Fatalf("catalog accepted unrelated environment secret: %v", err)
	}
}

func TestCatalogEvolutionPinsTrustFieldsAndVersionAliases(t *testing.T) {
	modelsOnly := cloneCatalog(t, &embeddedCatalog)
	modelsOnly.Revision = "2026-07-19.1"
	modelsOnly.Providers[0].Models = append(modelsOnly.Providers[0].Models, Model{ID: "glm-next", Tag: "glmnext", Source: "official"})
	modelsOnly.Providers[0].Models[0].Latest = false
	modelsOnly.Providers[0].Models[len(modelsOnly.Providers[0].Models)-1].Latest = true
	if err := validateCatalog(modelsOnly); err != nil {
		t.Fatal(err)
	}
	if err := validateCatalogEvolution(&embeddedCatalog, modelsOnly); err != nil {
		t.Fatalf("safe model update rejected: %v", err)
	}
	retired := cloneCatalog(t, &embeddedCatalog)
	retired.Revision = "2026-07-19.2"
	retired.RetiredTags["glm47"] = catalogTagTrustIndex(&embeddedCatalog)["glm47"]
	retired.Providers[0].Models = retired.Providers[0].Models[:2] // retire glm-4.7 / glm47
	if err := validateCatalog(retired); err != nil {
		t.Fatal(err)
	}
	if err := validateCatalogEvolution(&embeddedCatalog, retired); err != nil {
		t.Fatalf("retired model deletion rejected: %v", err)
	}
	routeChange := cloneCatalog(t, &embeddedCatalog)
	routeChange.Providers[0].OpenAIURL = "https://redirected.example"
	if err := validateCatalogEvolution(&embeddedCatalog, routeChange); err == nil {
		t.Fatal("endpoint change was accepted as an automatic catalog update")
	}
	tagChange := cloneCatalog(t, &embeddedCatalog)
	tagChange.Providers[0].Models[0].ID = "different-model"
	if err := validateCatalogEvolution(&embeddedCatalog, tagChange); err == nil {
		t.Fatal("pinned version alias was rebound")
	}

	shortChange := cloneCatalog(t, &embeddedCatalog)
	for providerIndex := range shortChange.Providers {
		if shortChange.Providers[providerIndex].Alias != "sf" {
			continue
		}
		for modelIndex := range shortChange.Providers[providerIndex].Models {
			if shortChange.Providers[providerIndex].Models[modelIndex].Short == "k27" {
				shortChange.Providers[providerIndex].Models[modelIndex].ID = "different-kimi"
			}
		}
	}
	if err := validateCatalog(shortChange); err != nil {
		t.Fatal(err)
	}
	if err := validateCatalogEvolution(&embeddedCatalog, shortChange); err == nil || !strings.Contains(err.Error(), "短名") {
		t.Fatalf("scoped model short was rebound: %v", err)
	}

	duplicateOfficial := cloneCatalog(t, &embeddedCatalog)
	duplicateOfficial.Providers[0].Models[0].Short = "k3"
	if err := validateCatalog(duplicateOfficial); err == nil || !strings.Contains(err.Error(), "官方模型短名") {
		t.Fatalf("duplicate official short was accepted: %v", err)
	}

	ambiguousSelector := cloneCatalog(t, &embeddedCatalog)
	ambiguousSelector.Providers[0].Models[0].Tag = ambiguousSelector.Providers[0].Models[1].ID
	if err := validateCatalog(ambiguousSelector); err == nil || !strings.Contains(err.Error(), "模型选择名") {
		t.Fatalf("ambiguous provider-scoped selector was accepted: %v", err)
	}

	reusedRetired := cloneCatalog(t, &embeddedCatalog)
	reusedRetired.Providers[0].Models[0].Short = "kimic"
	if err := validateCatalog(reusedRetired); err != nil {
		t.Fatal(err)
	}
	if err := validateCatalogEvolution(&embeddedCatalog, reusedRetired); err == nil || !strings.Contains(err.Error(), "已退役") {
		t.Fatalf("retired tag became a new official short: %v", err)
	}
}

func TestVersionComparisonAndArgumentQuoting(t *testing.T) {
	if compareSemver("v1.2.0", "v1.1.9") <= 0 || compareSemver("v1.0.0", "v1.0.0") != 0 || compareSemver("v0.9.9", "v1.0.0") >= 0 {
		t.Fatal("semantic version comparison is incorrect")
	}
	if compareSemver("v1.0.0", "v1.0.0-rc.1") <= 0 || compareSemver("v1.0.0-rc.2", "v1.0.0-rc.10") >= 0 {
		t.Fatal("prerelease comparison is incorrect")
	}
	for _, invalid := range []string{" v1.0.0", "v1.0.0+", "v1.0.0-01", "v1.0.0+bad\x1b"} {
		if _, ok := parseSemver(invalid); ok {
			t.Fatalf("invalid semver accepted: %q", invalid)
		}
	}
	if got := joinArgs([]string{"fix the bug", "it's", "safe"}); got != `'fix the bug' 'it'"'"'s' safe` {
		t.Fatalf("quoted argv=%q", got)
	}
}

func TestReleaseCheckValidatesMetadata(t *testing.T) {
	t.Setenv("CX_INSTALL_URL", "https://example.com/install.sh")
	tag := "v9.0.0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	}))
	defer srv.Close()
	t.Setenv("CX_RELEASE_API_URL", srv.URL)
	r, err := checkRelease(context.Background())
	if err != nil || !r.Update || r.Latest != tag {
		t.Fatalf("release=%#v err=%v", r, err)
	}
	tag = "v9.0.0\x1b[31m"
	if _, err := checkRelease(context.Background()); err == nil {
		t.Fatal("release check accepted a terminal-control tag")
	}
	tag = "v9.0.0"
	t.Setenv("CX_INSTALL_URL", "http://public.example/install.sh")
	if _, err := checkRelease(context.Background()); err == nil {
		t.Fatal("release check accepted an insecure public install URL")
	}
}

func TestStartupUpdateIntervalAndSourceChange(t *testing.T) {
	isolatedConfig(t)
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	if !startupUpdateDue(now) {
		t.Fatal("first startup update was not due")
	}
	if err := recordStartupUpdateAttempt(now); err != nil {
		t.Fatal(err)
	}
	if !startupUpdateDue(now.Add(time.Second)) {
		t.Fatal("default did not check on every launch")
	}
	t.Setenv("CX_UPDATE_INTERVAL", "1h")
	if startupUpdateDue(now.Add(30 * time.Minute)) {
		t.Fatal("startup update ignored the configured interval")
	}
	if !startupUpdateDue(now.Add(61 * time.Minute)) {
		t.Fatal("startup update did not become due")
	}
	t.Setenv("CX_UPDATE_INTERVAL", "0")
	if !startupUpdateDue(now.Add(time.Second)) {
		t.Fatal("zero interval did not force every-launch checks")
	}
	t.Setenv("CX_UPDATE_INTERVAL", "1h")
	t.Setenv("CX_CATALOG_URL", "https://catalog.example/catalog.json")
	if !startupUpdateDue(now.Add(time.Second)) {
		t.Fatal("catalog source change did not force a check")
	}
}

func TestReleaseUpdateIntervalAndSourceChange(t *testing.T) {
	isolatedConfig(t)
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	if !releaseUpdateDue(now) {
		t.Fatal("first release update was not due")
	}
	if err := recordReleaseUpdateAttempt(now); err != nil {
		t.Fatal(err)
	}
	if releaseUpdateDue(now.Add(30 * time.Minute)) {
		t.Fatal("default release interval was not throttled")
	}
	if !releaseUpdateDue(now.Add(61 * time.Minute)) {
		t.Fatal("release update did not become due")
	}
	t.Setenv("CX_RELEASE_INTERVAL", "0")
	if !releaseUpdateDue(now.Add(time.Second)) {
		t.Fatal("zero release interval did not force every-launch checks")
	}
	t.Setenv("CX_RELEASE_INTERVAL", "1h")
	t.Setenv("CX_RELEASE_API_URL", "https://releases.example/latest")
	if !releaseUpdateDue(now.Add(time.Second)) {
		t.Fatal("release source change did not force a check")
	}
}

func TestStartupCatalogAndReleaseChecksStartInParallel(t *testing.T) {
	isolatedConfig(t)
	arrived := make(chan string, 2)
	gate := make(chan struct{})
	catalogServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- "catalog"
		select {
		case <-gate:
			_, _ = w.Write(embeddedCatalogJSON)
		case <-r.Context().Done():
		}
	}))
	defer catalogServer.Close()
	releaseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- "release"
		select {
		case <-gate:
			fmt.Fprintf(w, `{"tag_name":%q}`, appVersion)
		case <-r.Context().Done():
		}
	}))
	defer releaseServer.Close()
	t.Setenv("PROVIDERDECK_CATALOG_URL", catalogServer.URL)
	t.Setenv("PROVIDERDECK_RELEASE_API_URL", releaseServer.URL)
	t.Setenv("PROVIDERDECK_INSTALL_URL", releaseServer.URL+"/install.sh")

	done := make(chan struct{})
	go func() {
		checkUpdatesOnStartup()
		close(done)
	}()
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case endpoint := <-arrived:
			seen[endpoint] = true
		case <-time.After(time.Second):
			close(gate)
			<-done
			t.Fatalf("startup checks were not concurrent; arrived=%v", seen)
		}
	}
	close(gate)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("parallel startup checks did not finish after release")
	}
}

func TestManualCatalogUpdateDoesNotCheckRelease(t *testing.T) {
	isolatedConfig(t)
	catalogServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer catalogServer.Close()
	var releaseRequests atomic.Int32
	releaseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		releaseRequests.Add(1)
		fmt.Fprintf(w, `{"tag_name":%q}`, appVersion)
	}))
	defer releaseServer.Close()
	t.Setenv("PROVIDERDECK_CATALOG_URL", catalogServer.URL)
	t.Setenv("PROVIDERDECK_RELEASE_API_URL", releaseServer.URL)
	t.Setenv("PROVIDERDECK_INSTALL_URL", releaseServer.URL+"/install.sh")

	if err := runUpdateCommand(nil); err == nil {
		t.Fatalf("manual update did not report catalog failure: %v", err)
	}
	if got := releaseRequests.Load(); got != 0 {
		t.Fatalf("catalog-only update made %d release requests", got)
	}
}

func TestUpdateModesAndAllContinuesAfterFailure(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want updateMode
	}{
		{nil, updateCatalog},
		{[]string{"--tool"}, updateTools},
		{[]string{"--self"}, updateSelf},
		{[]string{"--all"}, updateAll},
	} {
		got, err := parseUpdateMode(tc.args)
		if err != nil || got != tc.want {
			t.Fatalf("parseUpdateMode(%v) = %v, %v; want %v", tc.args, got, err, tc.want)
		}
	}
	for _, args := range [][]string{{"--unknown"}, {"--tool", "--self"}} {
		if _, err := parseUpdateMode(args); err == nil {
			t.Fatalf("parseUpdateMode(%v) accepted invalid arguments", args)
		}
	}

	var order []string
	var err error
	output := captureStdout(t, func() {
		err = executeUpdate(updateAll, updateRunners{
			catalog: func() error {
				order = append(order, "catalog")
				return errors.New("offline")
			},
			tools: func() error {
				order = append(order, "tools")
				return nil
			},
			self: func() error {
				order = append(order, "self")
				return nil
			},
		})
	})
	if err == nil || !strings.Contains(err.Error(), "模型列表") {
		t.Fatalf("all-update error = %v", err)
	}
	if got := strings.Join(order, ","); got != "catalog,tools,self" {
		t.Fatalf("all-update order = %q", got)
	}
	for _, want := range []string{"[1/3] 更新模型列表", "[2/3] 更新 Codex、Claude Code 和 OpenCode", "[3/3] 更新 MuxLM"} {
		if !strings.Contains(output, want) {
			t.Fatalf("all-update output %q missing %q", output, want)
		}
	}
	if strings.Contains(output, "==") {
		t.Fatalf("all-update output uses internal-style headings: %q", output)
	}
}

func TestToolUpdateUsesOfficialUpdaterCommands(t *testing.T) {
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "updates.log")
	for _, name := range []string{"codex", "claude", "opencode"} {
		subcommand := "update"
		if name == "opencode" {
			subcommand = "upgrade"
		}
		script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--help\" ]; then printf '  %s\\n'; exit 0; fi\nprintf '%s:%%s\\n' \"$*\" >> \"$UPDATE_LOG\"\n", subcommand, name)
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("UPDATE_LOG", logPath)
	var runErr error
	output := captureStdout(t, func() { runErr = runToolUpdates() })
	if runErr != nil {
		t.Fatal(runErr)
	}
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{"codex:update\n", "claude:update\n", "opencode:upgrade\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("tool update log %q missing %q", got, want)
		}
	}
	for _, want := range []string{"正在更新 Codex", "正在更新 Claude Code", "正在更新 OpenCode"} {
		if !strings.Contains(output, want) {
			t.Fatalf("tool update output %q missing %q", output, want)
		}
	}
	for _, unwanted := range []string{"updater", "codex update", "AI 工具"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("tool update output %q contains %q", output, unwanted)
		}
	}
}

func TestToolUpdateContinuesAfterFailure(t *testing.T) {
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "updates.log")
	for _, item := range []struct {
		name string
		exit int
	}{{"codex", 7}, {"claude", 0}, {"opencode", 0}} {
		subcommand := "update"
		if item.name == "opencode" {
			subcommand = "upgrade"
		}
		script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--help\" ]; then printf '  %s\\n'; exit 0; fi\nprintf '%s\\n' >> \"$UPDATE_LOG\"\nexit %d\n", subcommand, item.name, item.exit)
		if err := os.WriteFile(filepath.Join(bin, item.name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("UPDATE_LOG", logPath)
	err := runToolUpdates()
	if err == nil || !strings.Contains(err.Error(), "Codex") {
		t.Fatalf("tool update error = %v", err)
	}
	b, readErr := os.ReadFile(logPath)
	if readErr != nil || string(b) != "codex\nclaude\nopencode\n" {
		t.Fatalf("tool update log = %q, %v", b, readErr)
	}
}

func TestToolUpdateDoesNotLaunchToolWithoutOfficialUpdater(t *testing.T) {
	bin := t.TempDir()
	launched := filepath.Join(t.TempDir(), "launched")
	script := "#!/bin/sh\nif [ \"$1\" = \"--help\" ]; then printf 'Usage: codex [PROMPT]\\n'; exit 0; fi\nprintf launched > \"$LAUNCHED\"\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("LAUNCHED", launched)
	err := runToolUpdates()
	if err == nil || !strings.Contains(err.Error(), "Codex") {
		t.Fatalf("missing updater error = %v", err)
	}
	if _, statErr := os.Stat(launched); !os.IsNotExist(statErr) {
		t.Fatalf("tool without updater was launched: %v", statErr)
	}
}

func TestSelfUpdateRunsInstallerForManagedBinary(t *testing.T) {
	isolatedConfig(t)
	installDir := t.TempDir()
	executable := filepath.Join(installDir, binaryName)
	if err := os.WriteFile(executable, []byte("managed binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, ".muxlm-install.sha256"), []byte(strings.Repeat("a", 64)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "self-update")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			fmt.Fprint(w, `{"tag_name":"v9.0.0"}`)
		case "/install.sh":
			_, _ = io.WriteString(w, "#!/usr/bin/env bash\nprintf '%s|%s' \"$BINDIR\" \"$FORCE\" > \"$SELF_UPDATE_CAPTURE\"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("MUXLM_RELEASE_API_URL", srv.URL+"/release")
	t.Setenv("MUXLM_INSTALL_URL", srv.URL+"/install.sh")
	t.Setenv("SELF_UPDATE_CAPTURE", capture)
	t.Setenv("BINDIR", "/should/not/be/used")
	t.Setenv("FORCE", "1")
	var selfErr error
	output := captureStdout(t, func() { selfErr = runSelfUpdateForExecutable(executable) })
	if selfErr != nil {
		t.Fatal(selfErr)
	}
	b, err := os.ReadFile(capture)
	realInstallDir, evalErr := filepath.EvalSymlinks(installDir)
	if err != nil || evalErr != nil || string(b) != realInstallDir+"|0" {
		t.Fatalf("self updater env = %q, %v", b, err)
	}
	if !strings.Contains(output, "正在更新 MuxLM") || !strings.Contains(output, "MuxLM 已更新至") || strings.Contains(output, "installer") {
		t.Fatalf("self update output = %q", output)
	}
}

func TestSelfUpdateRejectsUnmanagedBinary(t *testing.T) {
	isolatedConfig(t)
	executable := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(executable, []byte("unmanaged"), 0o700); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.0.0"}`)
	}))
	defer srv.Close()
	t.Setenv("MUXLM_RELEASE_API_URL", srv.URL)
	t.Setenv("MUXLM_INSTALL_URL", srv.URL+"/install.sh")
	if err := runSelfUpdateForExecutable(executable); err == nil || !strings.Contains(err.Error(), "当前安装方式不支持自动更新") {
		t.Fatalf("unmanaged self update error = %v", err)
	}
}

func TestIntlAvailabilityIsProtocolSpecific(t *testing.T) {
	p := &Provider{
		ClaudeURL:     "https://anthropic.example",
		OpenAIURL:     "https://openai.example",
		OpenAIURLIntl: "https://openai-intl.example",
	}
	if p.hasIntlFor("claude") || !p.hasIntlFor("codex") || !p.hasIntlFor("opencode") {
		t.Fatalf("protocol-specific intl detection is incorrect")
	}
}

func cloneCatalog(t *testing.T, source *CatalogFile) *CatalogFile {
	t.Helper()
	b, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var clone CatalogFile
	if err := json.Unmarshal(b, &clone); err != nil {
		t.Fatal(err)
	}
	return &clone
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
	p := buildIndex()["m27std"].Prov
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

func TestProbeRejectsCrossDomainRedirectBeforeSendingKey(t *testing.T) {
	var leaked atomic.Bool
	sink := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "" || r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
	}))
	defer sink.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL+"/collect", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	probe("anthropic", origin.URL, "model", "must-not-leak")
	if leaked.Load() {
		t.Fatal("probe forwarded a key across hosts")
	}
}

func TestHelpIsOnlyRecognizedAsTheFirstArgument(t *testing.T) {
	if !isHelpCommand([]string{"--help"}) || !isHelpCommand([]string{"help"}) {
		t.Fatal("top-level help was not detected")
	}
	for _, args := range [][]string{{"m27std", "help"}, {"m27std", "--help"}, {"m27std", "--", "--help"}} {
		if isHelpCommand(args) {
			t.Fatalf("provider arguments were intercepted as wrapper help: %v", args)
		}
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
