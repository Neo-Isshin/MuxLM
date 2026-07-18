package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	defaultCatalogURL   = "https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/catalog.json"
	maxCatalogBytes     = 2 << 20
	catalogStateVersion = 2
)

type catalogUpdateState struct {
	Version       int               `json:"version"`
	URL           string            `json:"url"`
	ETag          string            `json:"etag,omitempty"`
	LastModified  string            `json:"last_modified,omitempty"`
	Revision      string            `json:"revision,omitempty"`
	SHA256        string            `json:"sha256,omitempty"`
	CatalogDigest string            `json:"catalog_digest,omitempty"`
	TagTargets    map[string]string `json:"tag_targets,omitempty"`
	RetiredTags   map[string]bool   `json:"retired_tags,omitempty"`
}

type catalogUpdateResult struct {
	Updated       bool
	NotModified   bool
	Revision      string
	ProviderCount int
	SHA256        string
}

func catalogURL() string {
	if u := firstEnv("MUXLM_CATALOG_URL", "PROVIDERDECK_CATALOG_URL", "CX_CATALOG_URL"); u != "" {
		return u
	}
	return defaultCatalogURL
}

func validateCatalog(c *CatalogFile) error {
	if c.Version != 1 {
		return fmt.Errorf("不支持的 catalog version: %d", c.Version)
	}
	if strings.TrimSpace(c.Revision) == "" {
		return fmt.Errorf("catalog 缺少 revision")
	}
	if _, _, ok := parseCatalogRevision(c.Revision); !ok {
		return fmt.Errorf("catalog revision 格式无效（应为 YYYY-MM-DD.N）: %q", c.Revision)
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("catalog 没有 provider")
	}
	if len(c.Providers) > 512 {
		return fmt.Errorf("catalog provider 数量超过 512")
	}
	names := map[string]bool{}
	providerPlans := map[string]bool{}
	keyEnvOwners := map[string]string{}
	for i := range c.Providers {
		p := &c.Providers[i]
		if !validCatalogID(p.ID, 64) {
			return fmt.Errorf("非法 provider id: %q", p.ID)
		}
		if strings.HasPrefix(p.ID, "custom-") || p.planID() == "custom" {
			return fmt.Errorf("%s 使用了为本地自定义 provider 保留的 namespace", p.Alias)
		}
		if !validCatalogID(p.Alias, 64) {
			return fmt.Errorf("非法 alias: %q", p.Alias)
		}
		if names[p.Alias] {
			return fmt.Errorf("重复 alias: %s", p.Alias)
		}
		if isReservedAlias(p.Alias) {
			return fmt.Errorf("alias 是保留命令: %s", p.Alias)
		}
		names[p.Alias] = true
		if p.Plan != "" && !validCatalogID(p.Plan, 32) {
			return fmt.Errorf("%s 含非法 plan: %q", p.Alias, p.Plan)
		}
		providerPlan := p.ID + "/" + p.planID()
		if providerPlans[providerPlan] {
			return fmt.Errorf("重复 provider/plan: %s", providerPlan)
		}
		providerPlans[providerPlan] = true
		if !validCatalogText(p.Name, 128) {
			return fmt.Errorf("%s 缺少 name", p.Alias)
		}
		if !validCatalogKeyEnv(p) {
			return fmt.Errorf("%s 含非法或保留的 key_env: %q", p.Alias, p.KeyEnv)
		}
		if owner, exists := keyEnvOwners[p.KeyEnv]; exists {
			return fmt.Errorf("key_env %s 同时属于 %s 和 %s", p.KeyEnv, owner, providerPlan)
		}
		keyEnvOwners[p.KeyEnv] = providerPlan
		if len(p.CLI) == 0 {
			return fmt.Errorf("%s 没有可用 CLI", p.Alias)
		}
		seenCLI := map[string]bool{}
		for _, cli := range p.CLI {
			if cli != "claude" && cli != "codex" && cli != "opencode" {
				return fmt.Errorf("%s 含未知 CLI: %q", p.Alias, cli)
			}
			if seenCLI[cli] {
				return fmt.Errorf("%s 含重复 CLI: %s", p.Alias, cli)
			}
			seenCLI[cli] = true
		}
		if p.WireAPI != "" && p.WireAPI != "chat" && p.WireAPI != "responses" {
			return fmt.Errorf("%s 含未知 wire_api: %q", p.Alias, p.WireAPI)
		}
		if p.WireAPI != "" && p.OpenAIURL == "" && p.OpenAIURLIntl == "" {
			return fmt.Errorf("%s 设置了 wire_api 但没有 OpenAI 端点", p.Alias)
		}
		if p.ClaudeURL == "" && p.OpenAIURL == "" && p.ClaudeURLIntl == "" && p.OpenAIURLIntl == "" {
			return fmt.Errorf("%s 没有端点", p.Alias)
		}
		if seenCLI["claude"] && p.ClaudeURL == "" && p.ClaudeURLIntl == "" {
			return fmt.Errorf("%s 声明支持 claude 但没有 Anthropic 端点", p.Alias)
		}
		if seenCLI["codex"] && p.OpenAIURL == "" && p.OpenAIURLIntl == "" {
			return fmt.Errorf("%s 声明支持 codex 但没有 OpenAI 端点", p.Alias)
		}
		if p.ClaudeURLIntl != "" && p.ClaudeURL == "" {
			return fmt.Errorf("%s 的 Anthropic 海外端点缺少国内/默认端点", p.Alias)
		}
		if p.OpenAIURLIntl != "" && p.OpenAIURL == "" {
			return fmt.Errorf("%s 的 OpenAI 海外端点缺少国内/默认端点", p.Alias)
		}
		if len(p.Models) == 0 {
			return fmt.Errorf("%s 没有模型", p.Alias)
		}
		if len(p.Models) > 512 {
			return fmt.Errorf("%s 的模型数量超过 512", p.Alias)
		}
		latest := 0
		for _, m := range p.Models {
			if !validCatalogText(m.ID, 256) {
				return fmt.Errorf("%s 含空 model id", p.Alias)
			}
			if m.Latest {
				latest++
			}
			if m.Tag != "" {
				if !validCatalogID(m.Tag, 64) {
					return fmt.Errorf("%s 含非法模型别名: %q", p.Alias, m.Tag)
				}
				if isReservedAlias(m.Tag) {
					return fmt.Errorf("模型别名是保留命令: %s", m.Tag)
				}
				if names[m.Tag] {
					return fmt.Errorf("重复模型别名: %s", m.Tag)
				}
				names[m.Tag] = true
			}
		}
		if latest != 1 {
			return fmt.Errorf("%s 必须且只能有一个 latest 模型", p.Alias)
		}
		for _, endpoint := range []string{p.ClaudeURL, p.OpenAIURL, p.ClaudeURLIntl, p.OpenAIURLIntl} {
			if endpoint == "" {
				continue
			}
			if err := validateEndpoint(endpoint, false); err != nil {
				return fmt.Errorf("%s: %w", p.Alias, err)
			}
		}
	}
	for tag, target := range c.RetiredTags {
		if !validCatalogID(tag, 64) || isReservedAlias(tag) {
			return fmt.Errorf("非法 retired model alias: %q", tag)
		}
		if !validCatalogText(target, 512) {
			return fmt.Errorf("retired model alias %s 缺少有效的历史目标", tag)
		}
		if names[tag] {
			return fmt.Errorf("retired model alias %s 仍被活动条目占用", tag)
		}
	}
	return nil
}

func validCatalogID(value string, max int) bool {
	return value != "" && len(value) <= max && safeID(value) == value
}

func validCatalogText(value string, max int) bool {
	trimmed := strings.TrimSpace(value)
	if value != trimmed || value == "" || len(value) > max {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == 0x1b {
			return false
		}
	}
	return true
}

var legacyCatalogKeyEnvs = map[string]string{
	"glm/standard":         "GLM_KEY",
	"glm/coding":           "GLM_CODING_KEY",
	"kimi/standard":        "KIMI_KEY",
	"kimi/coding":          "KIMI_CODING_KEY",
	"minimax/standard":     "MINIMAX_KEY",
	"doubao/coding":        "ARK_API_KEY",
	"nvidia/standard":      "NVIDIA_API_KEY",
	"deepseek/standard":    "DEEPSEEK_API_KEY",
	"siliconflow/standard": "SILICONFLOW_KEY",
}

func validCatalogKeyEnv(p *Provider) bool {
	if !validEnvName(p.KeyEnv) {
		return false
	}
	identity := p.ID + "/" + p.planID()
	if expected, known := legacyCatalogKeyEnvs[identity]; known {
		return p.KeyEnv == expected
	}
	suffix := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(p.ID))
	for _, namespace := range []string{"MUXLM_PROVIDER_", "PROVIDERDECK_PROVIDER_", "CX_PROVIDER_"} {
		prefix := namespace + suffix
		if p.KeyEnv == prefix+"_KEY" || strings.HasPrefix(p.KeyEnv, prefix+"_") && strings.HasSuffix(p.KeyEnv, "_KEY") {
			return true
		}
	}
	return false
}

func validateEndpoint(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("无效端点 URL: %q", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("端点不能包含凭据、query 或 fragment")
	}
	local := isLoopbackHost(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (local || allowInsecure)) {
		return fmt.Errorf("端点必须使用 HTTPS（本机地址除外）: %s", raw)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateUpdateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("无效 catalog URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return nil, fmt.Errorf("catalog 更新只允许 HTTPS（本机地址除外）")
	}
	return u, nil
}

func updateHTTPClient(origin *url.URL) *http.Client {
	return &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("重定向过多")
		}
		if req.URL.Scheme != "https" && !(req.URL.Scheme == "http" && isLoopbackHost(req.URL.Hostname())) {
			return fmt.Errorf("拒绝不安全重定向")
		}
		if !strings.EqualFold(req.URL.Hostname(), origin.Hostname()) {
			return fmt.Errorf("拒绝跨域更新重定向")
		}
		return nil
	}}
}

func loadCatalogUpdateState() catalogUpdateState {
	var state catalogUpdateState
	b, err := readPrivateFile(updateStateFile())
	if err != nil || json.Unmarshal(b, &state) != nil || state.Version != catalogStateVersion {
		return catalogUpdateState{Version: catalogStateVersion}
	}
	return state
}

func decodeCatalog(b []byte) (*CatalogFile, error) {
	var c CatalogFile
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("catalog JSON 无效: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return nil, fmt.Errorf("catalog JSON 无效: %w", err)
	}
	if err := validateCatalog(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON 后含有额外内容")
		}
		return err
	}
	return nil
}

func catalogDigest(c *CatalogFile) string {
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type providerTrustFields struct {
	Alias         string
	KeyEnv        string
	ClaudeURL     string
	OpenAIURL     string
	ClaudeURLIntl string
	OpenAIURLIntl string
	WireAPI       string
	CLIMask       uint8
}

func catalogTrustIndex(c *CatalogFile) map[string]providerTrustFields {
	out := make(map[string]providerTrustFields, len(c.Providers))
	for i := range c.Providers {
		p := &c.Providers[i]
		var cliMask uint8
		if p.supports("claude") {
			cliMask |= 1
		}
		if p.supports("codex") {
			cliMask |= 2
		}
		if p.supports("opencode") {
			cliMask |= 4
		}
		out[p.ID+"/"+p.planID()] = providerTrustFields{
			Alias:         p.Alias,
			KeyEnv:        p.KeyEnv,
			ClaudeURL:     p.ClaudeURL,
			OpenAIURL:     p.OpenAIURL,
			ClaudeURLIntl: p.ClaudeURLIntl,
			OpenAIURLIntl: p.OpenAIURLIntl,
			WireAPI:       p.wireAPI(),
			CLIMask:       cliMask,
		}
	}
	return out
}

func catalogTagTrustIndex(c *CatalogFile) map[string]string {
	out := make(map[string]string)
	for i := range c.Providers {
		p := &c.Providers[i]
		identity := p.ID + "/" + p.planID()
		for _, model := range p.Models {
			if model.Tag != "" {
				out[model.Tag] = identity + "/" + model.ID
			}
		}
	}
	return out
}

func catalogRetiredTagTrustIndex(c *CatalogFile) map[string]string {
	out := make(map[string]string, len(c.RetiredTags))
	for tag, target := range c.RetiredTags {
		out[tag] = target
	}
	return out
}

func catalogTagHistory(state catalogUpdateState, current *CatalogFile) map[string]string {
	history := catalogTagTrustIndex(&embeddedCatalog)
	for tag, target := range catalogRetiredTagTrustIndex(&embeddedCatalog) {
		history[tag] = target
	}
	for tag, target := range state.TagTargets {
		history[tag] = target
	}
	for tag, target := range catalogTagTrustIndex(current) {
		history[tag] = target
	}
	for tag, target := range catalogRetiredTagTrustIndex(current) {
		history[tag] = target
	}
	return history
}

func validateCatalogTagHistory(state catalogUpdateState, current, next *CatalogFile) error {
	history := catalogTagHistory(state, current)
	for tag, target := range catalogRetiredTagTrustIndex(next) {
		if previous, exists := history[tag]; exists && previous != target {
			return fmt.Errorf("退役版本别名 %s 的历史目标不能改变", tag)
		}
	}
	for tag, target := range catalogTagTrustIndex(next) {
		if state.RetiredTags[tag] || next.RetiredTags[tag] != "" {
			return fmt.Errorf("版本别名 %s 已退役，不能重新发布；请使用新别名", tag)
		}
		if previous, exists := history[tag]; exists && previous != target {
			return fmt.Errorf("版本别名 %s 不能重绑到另一模型；请使用新别名", tag)
		}
	}
	return nil
}

func advanceCatalogTagState(state catalogUpdateState, current, next *CatalogFile) (map[string]string, map[string]bool) {
	history := catalogTagHistory(state, current)
	retired := make(map[string]bool, len(state.RetiredTags))
	for tag, value := range state.RetiredTags {
		if value {
			retired[tag] = true
		}
	}
	for tag, target := range catalogRetiredTagTrustIndex(current) {
		history[tag] = target
		retired[tag] = true
	}
	for tag, target := range catalogRetiredTagTrustIndex(next) {
		history[tag] = target
		retired[tag] = true
	}
	nextTags := catalogTagTrustIndex(next)
	for tag := range catalogTagTrustIndex(current) {
		if _, remains := nextTags[tag]; !remains {
			retired[tag] = true
		}
	}
	for tag, target := range nextTags {
		history[tag] = target
	}
	return history, retired
}

// Catalog auto-updates may move models/latest aliases, but they cannot silently
// redirect an existing provider key. Endpoint/key identity changes require a
// reviewed binary release with a new embedded seed.
func validateCatalogEvolution(current, next *CatalogFile) error {
	trusted := catalogTrustIndex(current)
	nextTrust := catalogTrustIndex(next)
	for identity := range trusted {
		if _, exists := nextTrust[identity]; !exists {
			return fmt.Errorf("catalog 不能删除已有 provider identity %s；请先通过程序版本更新", identity)
		}
	}
	for identity, fields := range nextTrust {
		if previous, exists := trusted[identity]; exists && previous != fields {
			return fmt.Errorf("%s 的端点或 key identity 发生变化；请通过程序版本更新", identity)
		}
	}
	currentTags := catalogTagTrustIndex(current)
	nextTags := catalogTagTrustIndex(next)
	currentRetired := catalogRetiredTagTrustIndex(current)
	nextRetired := catalogRetiredTagTrustIndex(next)
	for tag, target := range currentRetired {
		if nextTarget, exists := nextRetired[tag]; !exists || nextTarget != target {
			return fmt.Errorf("catalog 必须保留退役版本别名 %s 的 tombstone", tag)
		}
	}
	for tag, target := range currentTags {
		// Retired models (and their tags) may be removed. A tag that remains,
		// however, must keep pointing at the same provider/model identity.
		if nextTarget, exists := nextTags[tag]; exists {
			if nextTarget != target {
				return fmt.Errorf("版本别名 %s 不能重绑到另一模型；请使用新别名", tag)
			}
			continue
		}
		if retiredTarget, exists := nextRetired[tag]; !exists || retiredTarget != target {
			return fmt.Errorf("删除模型别名 %s 时必须保留其 retired_tags tombstone", tag)
		}
	}
	return nil
}

func parseCatalogRevision(revision string) (time.Time, int, bool) {
	i := strings.LastIndexByte(revision, '.')
	if i <= 0 || i == len(revision)-1 {
		return time.Time{}, 0, false
	}
	day, err := time.Parse("2006-01-02", revision[:i])
	if err != nil {
		return time.Time{}, 0, false
	}
	sequence, err := strconv.Atoi(revision[i+1:])
	if err != nil || sequence < 0 || len(revision[i+1:]) > 1 && revision[i+1] == '0' {
		return time.Time{}, 0, false
	}
	return day, sequence, true
}

func compareCatalogRevision(a, b string) int {
	aDay, aSequence, aOK := parseCatalogRevision(a)
	bDay, bSequence, bOK := parseCatalogRevision(b)
	if !aOK || !bOK {
		return strings.Compare(a, b)
	}
	if aDay.Before(bDay) {
		return -1
	}
	if aDay.After(bDay) {
		return 1
	}
	if aSequence < bSequence {
		return -1
	}
	if aSequence > bSequence {
		return 1
	}
	return 0
}

// checkCatalogUpdate performs one conditional network check. Startup calls it
// with conditional=true; an invalid/missing local cache deliberately disables
// validators so a 304 can never preserve a broken catalog.
func checkCatalogUpdate(ctx context.Context, conditional bool) (catalogUpdateResult, error) {
	lock, acquired, err := acquireUpdateLock(ctx, !conditional)
	if err != nil {
		return catalogUpdateResult{}, err
	}
	if !acquired {
		if c, cacheErr := loadCachedCatalog(); cacheErr == nil {
			return catalogUpdateResult{NotModified: true, Revision: c.Revision, ProviderCount: len(c.Providers)}, nil
		}
		return catalogUpdateResult{}, fmt.Errorf("另一个进程正在更新 catalog")
	}
	defer releaseUpdateLock(lock)
	return checkCatalogUpdateLocked(ctx, conditional)
}

func acquireUpdateLock(ctx context.Context, wait bool) (*os.File, bool, error) {
	for {
		lock, acquired, err := tryUpdateLock()
		if err != nil || acquired || !wait {
			return lock, acquired, err
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, false, ctx.Err()
		case <-timer.C:
		}
	}
}

func tryUpdateLock() (*os.File, bool, error) {
	if err := ensurePrivateDir(configDir()); err != nil {
		return nil, false, err
	}
	path := updateLockFile()
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
			return nil, false, fmt.Errorf("拒绝使用非普通文件更新锁: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if err == unix.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return f, true, nil
}

func releaseUpdateLock(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	_ = f.Close()
}

func checkCatalogUpdateLocked(ctx context.Context, conditional bool) (catalogUpdateResult, error) {
	raw := catalogURL()
	u, err := validateUpdateURL(raw)
	if err != nil {
		return catalogUpdateResult{}, err
	}
	state := loadCatalogUpdateState()
	cache, cacheErr := loadCachedCatalog()
	cacheDigest := ""
	if cacheErr == nil {
		cacheDigest = catalogDigest(cache)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return catalogUpdateResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", binaryName+"/"+strings.TrimPrefix(appVersion, "v"))
	sentConditional := false
	if conditional && cacheErr == nil && state.URL == raw && state.Revision == cache.Revision && state.CatalogDigest == cacheDigest {
		if state.ETag != "" {
			req.Header.Set("If-None-Match", state.ETag)
			sentConditional = true
		}
		if state.LastModified != "" {
			req.Header.Set("If-Modified-Since", state.LastModified)
			sentConditional = true
		}
	}
	resp, err := updateHTTPClient(u).Do(req)
	if err != nil {
		return catalogUpdateResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		if !sentConditional || cacheErr != nil || state.URL != raw || state.Revision != cache.Revision || state.CatalogDigest != cacheDigest {
			return catalogUpdateResult{}, fmt.Errorf("服务器返回 304，但本地 catalog 不可用")
		}
		return catalogUpdateResult{NotModified: true, Revision: cache.Revision, ProviderCount: len(cache.Providers), SHA256: state.SHA256}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return catalogUpdateResult{}, fmt.Errorf("catalog 下载失败: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return catalogUpdateResult{}, err
	}
	if len(b) > maxCatalogBytes {
		return catalogUpdateResult{}, fmt.Errorf("catalog 超过 2 MiB 限制")
	}
	c, err := decodeCatalog(b)
	if err != nil {
		return catalogUpdateResult{}, err
	}
	currentRevision := activeCatalogRevision()
	if state.Revision != "" && compareCatalogRevision(state.Revision, currentRevision) > 0 {
		currentRevision = state.Revision
	}
	if compareCatalogRevision(c.Revision, currentRevision) < 0 {
		return catalogUpdateResult{}, fmt.Errorf("拒绝 catalog 回滚：远端 %s，当前 %s", c.Revision, currentRevision)
	}
	baseline := &embeddedCatalog
	if cacheErr == nil && compareCatalogRevision(cache.Revision, embeddedCatalog.Revision) >= 0 {
		baseline = cache
	}
	if err := validateCatalogEvolution(baseline, c); err != nil {
		return catalogUpdateResult{}, err
	}
	if err := validateCatalogTagHistory(state, baseline, c); err != nil {
		return catalogUpdateResult{}, err
	}
	sum := sha256.Sum256(b)
	sumHex := hex.EncodeToString(sum[:])
	if c.Revision == embeddedCatalog.Revision && !reflect.DeepEqual(&embeddedCatalog, c) {
		return catalogUpdateResult{}, fmt.Errorf("catalog revision %s 与内置版本内容不一致；请发布新 revision", c.Revision)
	}
	if cacheErr == nil && cache.Revision == c.Revision && !reflect.DeepEqual(cache, c) {
		return catalogUpdateResult{}, fmt.Errorf("catalog revision %s 的内容发生变化；请发布新 revision", c.Revision)
	}
	if state.Revision == c.Revision && state.CatalogDigest != "" && state.CatalogDigest != catalogDigest(c) {
		return catalogUpdateResult{}, fmt.Errorf("catalog revision %s 与历史内容不一致；请发布新 revision", c.Revision)
	}
	writeCache := cacheErr != nil || cache.Revision != c.Revision
	updated := compareCatalogRevision(c.Revision, activeCatalogRevision()) > 0 || cacheErr == nil && cache.Revision != c.Revision
	tagTargets, retiredTags := advanceCatalogTagState(state, baseline, c)
	newState := catalogUpdateState{
		Version:       catalogStateVersion,
		URL:           raw,
		ETag:          resp.Header.Get("ETag"),
		LastModified:  resp.Header.Get("Last-Modified"),
		Revision:      c.Revision,
		SHA256:        sumHex,
		CatalogDigest: catalogDigest(c),
		TagTargets:    tagTargets,
		RetiredTags:   retiredTags,
	}
	if err := atomicWriteJSON(updateStateFile(), &newState); err != nil {
		return catalogUpdateResult{}, err
	}
	if writeCache {
		if err := atomicWriteJSON(catalogCacheFile(), c); err != nil {
			return catalogUpdateResult{}, err
		}
	}
	return catalogUpdateResult{Updated: updated, Revision: c.Revision, ProviderCount: len(c.Providers), SHA256: sumHex}, nil
}

func runCatalogUpdate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := checkCatalogUpdate(ctx, false)
	if err != nil {
		return err
	}
	if r.Updated {
		fmt.Printf("✓ catalog 已更新：revision=%s providers=%d sha256=%s\n", r.Revision, r.ProviderCount, r.SHA256)
		return nil
	}
	fmt.Printf("✓ catalog 已是最新：revision=%s providers=%d\n", r.Revision, r.ProviderCount)
	return nil
}
