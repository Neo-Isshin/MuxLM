package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultCatalogURL = "https://gitea.nxc8335.cloud/nxc8335/ez-switch/raw/branch/main/catalog.json"

func catalogURL() string {
	if u := os.Getenv("CX_CATALOG_URL"); u != "" {
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
	if len(c.Providers) == 0 {
		return fmt.Errorf("catalog 没有 provider")
	}
	names := map[string]bool{}
	for i := range c.Providers {
		p := &c.Providers[i]
		if safeID(p.ID) != p.ID || p.ID == "" {
			return fmt.Errorf("非法 provider id: %q", p.ID)
		}
		if safeID(p.Alias) != p.Alias || p.Alias == "" {
			return fmt.Errorf("非法 alias: %q", p.Alias)
		}
		if names[p.Alias] {
			return fmt.Errorf("重复 alias: %s", p.Alias)
		}
		names[p.Alias] = true
		if p.KeyEnv == "" {
			return fmt.Errorf("%s 缺少 key_env", p.Alias)
		}
		if len(p.Models) == 0 {
			return fmt.Errorf("%s 没有模型", p.Alias)
		}
		latest := 0
		for _, m := range p.Models {
			if m.ID == "" {
				return fmt.Errorf("%s 含空 model id", p.Alias)
			}
			if m.Latest {
				latest++
			}
			if m.Tag != "" {
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
	return nil
}

func validateEndpoint(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("无效端点 URL: %q", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("端点不能包含凭据、query 或 fragment")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && (local || allowInsecure)) {
		return fmt.Errorf("端点必须使用 HTTPS（本机地址除外）: %s", raw)
	}
	return nil
}

func runCatalogUpdate() error {
	raw := catalogURL()
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("无效 catalog URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")) {
		return fmt.Errorf("catalog 更新只允许 HTTPS")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("重定向过多")
		}
		if req.URL.Scheme != "https" && req.URL.Hostname() != "localhost" && req.URL.Hostname() != "127.0.0.1" {
			return fmt.Errorf("拒绝不安全重定向")
		}
		if req.URL.Hostname() != u.Hostname() {
			return fmt.Errorf("拒绝跨域 catalog 重定向")
		}
		return nil
	}
	resp, err := client.Get(raw)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("catalog 下载失败: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if len(b) >= 2<<20 {
		return fmt.Errorf("catalog 超过 2 MiB 限制")
	}
	var c CatalogFile
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return fmt.Errorf("catalog JSON 无效: %w", err)
	}
	if err := validateCatalog(&c); err != nil {
		return err
	}
	if err := atomicWriteJSON(catalogCacheFile(), &c); err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	fmt.Printf("✓ catalog 已更新：revision=%s providers=%d sha256=%s\n", c.Revision, len(c.Providers), hex.EncodeToString(sum[:]))
	return nil
}
