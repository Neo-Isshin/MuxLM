package main

import (
	_ "embed"
	"fmt"
	"os"
	"reflect"
	"strings"
)

// Model is one concrete model exposed by a provider.
type Model struct {
	ID     string `json:"id"`
	Tag    string `json:"tag"`
	Latest bool   `json:"latest"`
}

// Provider describes protocol endpoints, key metadata, and model aliases.
type Provider struct {
	ID            string   `json:"id"`
	Alias         string   `json:"alias"`
	Name          string   `json:"name"`
	Plan          string   `json:"plan,omitempty"`
	ClaudeURL     string   `json:"claude_url,omitempty"`
	OpenAIURL     string   `json:"openai_url,omitempty"`
	ClaudeURLIntl string   `json:"claude_url_intl,omitempty"`
	OpenAIURLIntl string   `json:"openai_url_intl,omitempty"`
	KeyEnv        string   `json:"key_env"`
	Key           string   `json:"-"` // Legacy custom.json only; never accepted in a catalog.
	CLI           []string `json:"cli"`
	WireAPI       string   `json:"wire_api,omitempty"`
	Models        []Model  `json:"models"`
}

type CatalogFile struct {
	Version     int               `json:"version"`
	Revision    string            `json:"revision"`
	RetiredTags map[string]string `json:"retired_tags,omitempty"`
	Providers   []Provider        `json:"providers"`
}

// catalog.json is the single source for both the shipped offline seed and the
// separately hosted update artifact.
//
//go:embed catalog.json
var embeddedCatalogJSON []byte

var embeddedCatalog = mustLoadEmbeddedCatalog()

// providers remains the immutable scrub/fallback set. It intentionally does
// not follow remote catalog removals, so retired provider keys stay isolated.
var providers = embeddedCatalog.Providers

func mustLoadEmbeddedCatalog() CatalogFile {
	c, err := decodeCatalog(embeddedCatalogJSON)
	if err != nil {
		panic("embedded catalog is invalid: " + err.Error())
	}
	return *c
}

func (p *Provider) providerID() string {
	if p.ID != "" {
		return p.ID
	}
	return p.Alias
}

func (p *Provider) planID() string {
	if p.Plan != "" {
		return p.Plan
	}
	return "standard"
}

func (p *Provider) supports(cli string) bool {
	for _, candidate := range p.CLI {
		if candidate == cli {
			return true
		}
	}
	return false
}

func (p *Provider) hasIntl() bool {
	return p.ClaudeURLIntl != "" || p.OpenAIURLIntl != ""
}

func (p *Provider) hasIntlFor(cli string) bool {
	switch cli {
	case "claude":
		return p.ClaudeURLIntl != ""
	case "codex":
		return p.OpenAIURLIntl != ""
	default: // OpenCode prefers an OpenAI-compatible route when present.
		if p.OpenAIURL != "" || p.OpenAIURLIntl != "" {
			return p.OpenAIURLIntl != ""
		}
		return p.ClaudeURLIntl != ""
	}
}

func (p *Provider) claudeURL(intl bool) string {
	if intl && p.ClaudeURLIntl != "" {
		return p.ClaudeURLIntl
	}
	return p.ClaudeURL
}

func (p *Provider) openaiURL(intl bool) string {
	if intl && p.OpenAIURLIntl != "" {
		return p.OpenAIURLIntl
	}
	return p.OpenAIURL
}

func (p *Provider) wireAPI() string {
	if p.WireAPI == "" {
		return "chat"
	}
	return p.WireAPI
}

// probeTarget selects the protocol and endpoint used by the chosen CLI.
func (p *Provider) probeTarget(cli string, intl bool) (protocol, base string) {
	switch cli {
	case "claude":
		return "anthropic", p.claudeURL(intl)
	case "codex":
		return "openai", p.openaiURL(intl)
	default:
		if endpoint := p.openaiURL(intl); endpoint != "" {
			return "openai", endpoint
		}
		return "anthropic", p.claudeURL(intl)
	}
}

func (p *Provider) keyEnv(intl bool) string {
	if intl && p.hasIntl() {
		return p.KeyEnv + "_INTL"
	}
	return p.KeyEnv
}

func (p *Provider) hostFor(cli string, intl bool) string {
	_, endpoint := p.probeTarget(cli, intl)
	return hostOf(endpoint)
}

func hostOf(raw string) string {
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	if i := strings.IndexByte(raw, '/'); i >= 0 {
		return raw[:i]
	}
	return raw
}

func loadCachedCatalog() (*CatalogFile, error) {
	data, err := readPrivateFile(catalogCacheFile())
	if err != nil {
		return nil, err
	}
	catalog, err := decodeCatalog(data)
	if err != nil {
		return nil, err
	}
	if err := validateCachedCatalog(catalog); err != nil {
		return nil, err
	}
	return catalog, nil
}

// validateCachedCatalog is shared by runtime activation and doctor so they
// cannot disagree about rollback, revision immutability, or trust evolution.
func validateCachedCatalog(catalog *CatalogFile) error {
	if compareCatalogRevision(catalog.Revision, embeddedCatalog.Revision) < 0 {
		return fmt.Errorf("catalog cache %s 旧于内置版本 %s", catalog.Revision, embeddedCatalog.Revision)
	}
	if catalog.Revision == embeddedCatalog.Revision && !reflect.DeepEqual(catalog, &embeddedCatalog) {
		return fmt.Errorf("catalog cache 与同 revision 的内置 catalog 内容不一致")
	}
	if err := validateCatalogEvolution(&embeddedCatalog, catalog); err != nil {
		return fmt.Errorf("catalog cache 不满足内置信任约束: %w", err)
	}
	return nil
}

func activeCatalogRevision() string {
	if c, err := loadCachedCatalog(); err == nil {
		return c.Revision
	}
	return embeddedCatalog.Revision
}

// catalogProviders prefers a valid, non-rollback cache and otherwise uses the
// embedded seed. A broken update never makes the CLI unusable.
func catalogProviders() []Provider {
	c, err := loadCachedCatalog()
	if err == nil {
		return c.Providers
	}
	if !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "⚠ 本地 catalog 无效，已回退到内置版本")
	}
	return providers
}

type Resolved struct {
	Prov  *Provider
	Model *Model
}

// buildIndex resolves provider aliases to latest models and version tags to
// pinned models. Catalog entries win over colliding local custom aliases.
func buildIndex() map[string]Resolved {
	idx := make(map[string]Resolved)
	retiredTags := retiredCatalogTags()
	add := func(ps []Provider, custom bool) {
		for i := range ps {
			provider := &ps[i]
			if custom && retiredTags[provider.Alias] {
				fmt.Fprintf(os.Stderr, "⚠ 自定义别名 %q 与已退役 catalog 别名冲突，已忽略自定义项\n", provider.Alias)
				continue
			}
			if _, exists := idx[provider.Alias]; custom && exists {
				fmt.Fprintf(os.Stderr, "⚠ 自定义别名 %q 与 catalog 冲突，已忽略自定义项\n", provider.Alias)
				continue
			}
			var latest *Model
			for j := range provider.Models {
				model := &provider.Models[j]
				if model.Tag != "" {
					if custom && retiredTags[model.Tag] {
						continue
					}
					if _, exists := idx[model.Tag]; !custom || !exists {
						idx[model.Tag] = Resolved{provider, model}
					}
				}
				if model.Latest {
					latest = model
				}
			}
			if latest != nil {
				idx[provider.Alias] = Resolved{provider, latest}
			}
		}
	}
	add(catalogProviders(), false)
	add(loadCustomProfiles(), true)
	return idx
}

func retiredCatalogTags() map[string]bool {
	state := loadCatalogUpdateState()
	activeCatalog := &embeddedCatalog
	if cached, err := loadCachedCatalog(); err == nil {
		activeCatalog = cached
	}
	retired := make(map[string]bool, len(state.RetiredTags)+len(activeCatalog.RetiredTags))
	for tag := range activeCatalog.RetiredTags {
		retired[tag] = true
	}
	for tag, value := range state.RetiredTags {
		if value {
			retired[tag] = true
		}
	}
	active := catalogTagTrustIndex(activeCatalog)
	for tag := range state.TagTargets {
		if _, exists := active[tag]; !exists {
			retired[tag] = true
		}
	}
	return retired
}
