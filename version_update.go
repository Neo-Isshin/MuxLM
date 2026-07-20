package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Release builds may override appVersion with:
//
//	go build -ldflags "-X main.appVersion=v2.1.0"
var appVersion = "v2.1.0"

const (
	defaultReleaseAPIURL   = "https://api.github.com/repos/Neo-Isshin/MuxLM/releases/latest"
	defaultInstallURL      = "https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh"
	startupUpdateTimeout   = 2500 * time.Millisecond
	defaultUpdateInterval  = time.Duration(0)
	defaultReleaseInterval = time.Hour
	maxReleaseJSONBytes    = 256 << 10
	maxInstallerBytes      = 1 << 20
)

type releaseCheckResult struct {
	Latest     string
	Update     bool
	InstallURL string
}

type startupCheckState struct {
	Version    int       `json:"version"`
	CheckedAt  time.Time `json:"checked_at"`
	CatalogURL string    `json:"catalog_url"`
	// ReleaseAPIURL is retained only so older state files continue to decode.
	ReleaseAPIURL string `json:"release_api_url,omitempty"`
}

type releaseCheckState struct {
	Version       int       `json:"version"`
	CheckedAt     time.Time `json:"checked_at"`
	ReleaseAPIURL string    `json:"release_api_url"`
}

func releaseAPIURL() string {
	if u := firstEnv("MUXLM_RELEASE_API_URL", "PROVIDERDECK_RELEASE_API_URL", "CX_RELEASE_API_URL"); u != "" {
		return u
	}
	return defaultReleaseAPIURL
}

func installURL() string {
	if u := firstEnv("MUXLM_INSTALL_URL", "PROVIDERDECK_INSTALL_URL", "CX_INSTALL_URL"); u != "" {
		return u
	}
	return defaultInstallURL
}

func updatesDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(firstEnv("MUXLM_AUTO_UPDATE", "PROVIDERDECK_AUTO_UPDATE", "CX_AUTO_UPDATE")))
	return v == "0" || v == "false" || v == "off" || v == "no"
}

func updateDebugEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(firstEnv("MUXLM_UPDATE_DEBUG", "PROVIDERDECK_UPDATE_DEBUG", "CX_UPDATE_DEBUG")))
	return v == "1" || v == "true" || v == "on" || v == "yes"
}

func startupUpdateInterval() time.Duration {
	raw := strings.TrimSpace(firstEnv("MUXLM_UPDATE_INTERVAL", "PROVIDERDECK_UPDATE_INTERVAL", "CX_UPDATE_INTERVAL"))
	if raw == "" {
		return defaultUpdateInterval
	}
	if raw == "0" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return defaultUpdateInterval
	}
	return d
}

func releaseUpdateInterval() time.Duration {
	raw := strings.TrimSpace(firstEnv("MUXLM_RELEASE_INTERVAL", "PROVIDERDECK_RELEASE_INTERVAL", "CX_RELEASE_INTERVAL"))
	if raw == "" {
		return defaultReleaseInterval
	}
	if raw == "0" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return defaultReleaseInterval
	}
	return d
}

func startupUpdateDue(now time.Time) bool {
	interval := startupUpdateInterval()
	if interval == 0 {
		return true
	}
	var state startupCheckState
	b, err := readPrivateFile(updateCheckFile())
	if err != nil || json.Unmarshal(b, &state) != nil || state.Version != 1 ||
		state.CatalogURL != catalogURL() {
		return true
	}
	return now.Sub(state.CheckedAt) >= interval || now.Before(state.CheckedAt)
}

func recordStartupUpdateAttempt(now time.Time) error {
	return atomicWriteJSON(updateCheckFile(), &startupCheckState{
		Version:    1,
		CheckedAt:  now.UTC(),
		CatalogURL: catalogURL(),
	})
}

func releaseUpdateDue(now time.Time) bool {
	interval := releaseUpdateInterval()
	if interval == 0 {
		return true
	}
	var state releaseCheckState
	b, err := readPrivateFile(releaseCheckFile())
	if err != nil || json.Unmarshal(b, &state) != nil || state.Version != 1 ||
		state.ReleaseAPIURL != releaseAPIURL() {
		return true
	}
	return now.Sub(state.CheckedAt) >= interval || now.Before(state.CheckedAt)
}

func recordReleaseUpdateAttempt(now time.Time) error {
	return atomicWriteJSON(releaseCheckFile(), &releaseCheckState{
		Version:       1,
		CheckedAt:     now.UTC(),
		ReleaseAPIURL: releaseAPIURL(),
	})
}

func checkRelease(ctx context.Context) (releaseCheckResult, error) {
	raw := releaseAPIURL()
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return releaseCheckResult{}, fmt.Errorf("无效 release API URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return releaseCheckResult{}, fmt.Errorf("release 检查只允许 HTTPS（本机地址除外）")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return releaseCheckResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", binaryName+"/"+strings.TrimPrefix(appVersion, "v"))
	resp, err := updateHTTPClient(u).Do(req)
	if err != nil {
		return releaseCheckResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseCheckResult{}, fmt.Errorf("release 检查失败: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseJSONBytes+1))
	if err != nil {
		return releaseCheckResult{}, err
	}
	if len(b) > maxReleaseJSONBytes {
		return releaseCheckResult{}, fmt.Errorf("release 响应过大")
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(b, &rel); err != nil || strings.TrimSpace(rel.TagName) == "" {
		return releaseCheckResult{}, fmt.Errorf("release 响应无效")
	}
	if _, ok := parseSemver(rel.TagName); !ok {
		return releaseCheckResult{}, fmt.Errorf("release tag 不是有效 semver: %q", rel.TagName)
	}
	if _, ok := parseSemver(appVersion); !ok {
		return releaseCheckResult{}, fmt.Errorf("当前程序版本无效: %q", appVersion)
	}
	installer := installURL()
	if _, err := validateInstallURL(installer); err != nil {
		return releaseCheckResult{}, err
	}
	return releaseCheckResult{
		Latest:     rel.TagName,
		Update:     compareSemver(rel.TagName, appVersion) > 0,
		InstallURL: installer,
	}, nil
}

// checkUpdatesOnStartup checks the catalog on every launch by default and
// throttles GitHub release API checks separately. When both are due they run in
// parallel. The catalog result is awaited before alias resolution so a newly
// published model can be used by the same invocation. Failures are silent by
// default; both network requests share startupUpdateTimeout as their deadline.
func checkUpdatesOnStartup() {
	if updatesDisabled() {
		return
	}
	now := time.Now()
	catalogDue := startupUpdateDue(now)
	releaseDue := releaseUpdateDue(now)
	if !catalogDue && !releaseDue {
		return
	}
	if catalogDue && startupUpdateInterval() > 0 {
		if err := recordStartupUpdateAttempt(now); err != nil && updateDebugEnabled() {
			fmt.Fprintln(os.Stderr, "⚠ 无法记录更新检查时间:", err)
		}
	}
	if releaseDue {
		if err := recordReleaseUpdateAttempt(now); err != nil && updateDebugEnabled() {
			fmt.Fprintln(os.Stderr, "⚠ 无法记录版本检查时间:", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), startupUpdateTimeout)
	defer cancel()
	type catalogResult struct {
		result catalogUpdateResult
		err    error
	}
	type versionResult struct {
		result releaseCheckResult
		err    error
	}
	var catCh chan catalogResult
	var verCh chan versionResult
	if catalogDue {
		catCh = make(chan catalogResult, 1)
		go func() {
			r, err := checkCatalogUpdate(ctx, true)
			catCh <- catalogResult{result: r, err: err}
		}()
	}
	if releaseDue {
		verCh = make(chan versionResult, 1)
		go func() {
			r, err := checkRelease(ctx)
			verCh <- versionResult{result: r, err: err}
		}()
	}
	if catalogDue {
		cat := <-catCh
		if cat.err != nil {
			if updateDebugEnabled() {
				fmt.Fprintln(os.Stderr, "⚠ 模型列表自动更新失败：", cat.err)
			}
		} else if cat.result.Updated {
			fmt.Fprintf(os.Stderr, "↻ 模型列表已自动更新至 %s（%d 个服务商）\n", cat.result.Revision, cat.result.ProviderCount)
		}
	}
	if releaseDue {
		ver := <-verCh
		if ver.err != nil {
			if updateDebugEnabled() {
				fmt.Fprintln(os.Stderr, "⚠ 版本检查失败:", ver.err)
			}
		} else if ver.result.Update {
			printReleaseNotice(ver.result)
		}
	}
}

func printReleaseNotice(r releaseCheckResult) {
	fmt.Fprintf(os.Stderr, "↑ MuxLM %s 已发布（当前 %s），运行 `cld update --self` 即可更新。\n", r.Latest, appVersion)
}

func runSelfUpdate() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法确定当前 MuxLM 路径: %w", err)
	}
	return runSelfUpdateForExecutable(executable)
}

func runSelfUpdateForExecutable(executable string) error {
	_ = recordReleaseUpdateAttempt(time.Now())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	r, err := checkRelease(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("检查 MuxLM 更新失败：%w", err)
	}
	if !r.Update {
		fmt.Printf("✓ MuxLM 已是最新：%s\n", appVersion)
		return nil
	}

	installDir, err := managedSelfInstallDir(executable)
	if err != nil {
		return fmt.Errorf("MuxLM %s 已发布，但无法自动更新：%w", r.Latest, err)
	}

	downloadCtx, downloadCancel := context.WithTimeout(context.Background(), 30*time.Second)
	installerPath, err := downloadInstaller(downloadCtx, r.InstallURL)
	downloadCancel()
	if err != nil {
		return fmt.Errorf("下载安装程序失败：%w", err)
	}
	defer os.Remove(installerPath)

	bash, err := exec.LookPath("bash")
	if err != nil {
		return fmt.Errorf("自更新需要 bash，但 PATH 中未找到")
	}
	fmt.Printf("→ 正在更新 MuxLM（%s → %s）…\n", appVersion, r.Latest)
	// #nosec G204 -- installerPath is a private temporary file downloaded from
	// the validated HTTPS/loopback install URL; install.sh verifies the release
	// binary checksum before replacing the managed executable.
	cmd := exec.Command(bash, installerPath)
	cmd.Env = setEnvValue(setEnvValue(os.Environ(), "BINDIR", installDir), "FORCE", "0")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("MuxLM 更新失败：%w", err)
	}
	fmt.Printf("✓ MuxLM 已更新至 %s\n", r.Latest)
	return nil
}

func validateInstallURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("安装地址无效")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return nil, fmt.Errorf("安装地址必须使用 HTTPS（本机地址除外）")
	}
	return u, nil
}

func downloadInstaller(ctx context.Context, raw string) (string, error) {
	u, err := validateInstallURL(raw)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/x-shellscript, text/plain")
	req.Header.Set("User-Agent", binaryName+"/"+strings.TrimPrefix(appVersion, "v"))
	resp, err := updateHTTPClient(u).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("安装程序下载失败：HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxInstallerBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxInstallerBytes {
		return "", fmt.Errorf("安装程序文件过大")
	}
	f, err := os.CreateTemp("", "muxlm-install-*.sh")
	if err != nil {
		return "", err
	}
	path := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return "", err
	}
	if _, err := f.Write(b); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

func managedSelfInstallDir(executable string) (string, error) {
	realExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("无法解析当前 MuxLM 路径: %w", err)
	}
	realExecutable, err = filepath.Abs(realExecutable)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(realExecutable)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("无法安全更新当前 MuxLM 文件")
	}
	var markerName string
	switch filepath.Base(realExecutable) {
	case binaryName:
		markerName = ".muxlm-install.sha256"
	case "providerdeck":
		markerName = ".providerdeck-install.sha256"
	default:
		return "", fmt.Errorf("当前安装方式不支持自动更新，请按原来的方式重新安装 MuxLM")
	}
	dir := filepath.Dir(realExecutable)
	markerInfo, err := os.Lstat(filepath.Join(dir, markerName))
	if err != nil || !markerInfo.Mode().IsRegular() {
		return "", fmt.Errorf("当前安装方式不支持自动更新，请按原来的方式重新安装 MuxLM")
	}
	return dir, nil
}

func setEnvValue(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return append(out, prefix+value)
}

func printVersion() {
	fmt.Printf("%s %s\n模型列表 %s\n", appName, appVersion, activeCatalogRevision())
}

func compareSemver(a, b string) int {
	av, aok := parseSemver(a)
	bv, bok := parseSemver(b)
	if !aok || !bok {
		return 0
	}
	for i := 0; i < 3; i++ {
		if av.numbers[i] < bv.numbers[i] {
			return -1
		}
		if av.numbers[i] > bv.numbers[i] {
			return 1
		}
	}
	return comparePrerelease(av.prerelease, bv.prerelease)
}

type semVersion struct {
	numbers    [3]int
	prerelease []string
}

func parseSemver(v string) (semVersion, bool) {
	var out semVersion
	if v == "" || v != strings.TrimSpace(v) {
		return out, false
	}
	v = strings.TrimPrefix(v, "v")
	if strings.Count(v, "+") > 1 {
		return out, false
	}
	if i := strings.IndexByte(v, '+'); i >= 0 {
		if i == len(v)-1 || !validSemverIdentifiers(strings.Split(v[i+1:], "."), false) {
			return out, false
		}
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		if i == len(v)-1 {
			return out, false
		}
		out.prerelease = strings.Split(v[i+1:], ".")
		if !validSemverIdentifiers(out.prerelease, true) {
			return semVersion{}, false
		}
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return out, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out.numbers[i] = n
	}
	return out, true
}

func validSemverIdentifiers(identifiers []string, rejectNumericLeadingZero bool) bool {
	for _, identifier := range identifiers {
		if identifier == "" {
			return false
		}
		allNumeric := true
		for _, r := range identifier {
			if r < '0' || r > '9' {
				allNumeric = false
			}
		}
		if rejectNumericLeadingZero && allNumeric && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
		for _, r := range identifier {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

func comparePrerelease(a, b []string) int {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		return 1
	}
	if len(b) == 0 {
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		aNumber, aErr := strconv.Atoi(a[i])
		bNumber, bErr := strconv.Atoi(b[i])
		switch {
		case aErr == nil && bErr == nil:
			if aNumber < bNumber {
				return -1
			}
			if aNumber > bNumber {
				return 1
			}
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		default:
			if a[i] < b[i] {
				return -1
			}
			if a[i] > b[i] {
				return 1
			}
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}
