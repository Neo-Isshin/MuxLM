// catalog-audit compares MuxLM's curated catalog with models.dev.
//
// It is intentionally read-only. Upstream presence is useful evidence, but it
// does not prove that a model is available on a particular billing plan or
// compatible with every CLI protocol exposed by MuxLM.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	defaultSource = "https://models.dev/api.json"
	maxSourceSize = 64 << 20
)

type muxCatalog struct {
	Revision  string        `json:"revision"`
	Providers []muxProvider `json:"providers"`
}

type muxProvider struct {
	ID     string     `json:"id"`
	Alias  string     `json:"alias"`
	Plan   string     `json:"plan"`
	Models []muxModel `json:"models"`
}

type muxModel struct {
	ID string `json:"id"`
}

type upstreamProvider struct {
	ID     string                   `json:"id"`
	Name   string                   `json:"name"`
	Doc    string                   `json:"doc"`
	Models map[string]upstreamModel `json:"models"`
}

type upstreamModel struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	ReleaseDate string     `json:"release_date"`
	LastUpdated string     `json:"last_updated"`
	ToolCall    bool       `json:"tool_call"`
	Modalities  modalities `json:"modalities"`
}

type modalities struct {
	Output []string `json:"output"`
}

type sourceRule struct {
	Alias        string
	Upstream     string
	Prefixes     []string
	IDs          map[string]bool
	Ignore       map[string]string
	KnownMissing map[string]string
}

// The mapping is deliberately explicit. A new provider in models.dev must not
// silently become a trusted MuxLM route.
var sourceRules = []sourceRule{
	{Alias: "glm", Upstream: "zhipuai", Prefixes: []string{"glm-"}},
	{Alias: "glmc", Upstream: "zhipuai-coding-plan", Prefixes: []string{"glm-"}},
	{Alias: "k", Upstream: "moonshotai-cn", Prefixes: []string{"kimi-"}},
	{
		Alias:    "kc",
		Upstream: "kimi-for-coding",
		IDs:      stringSet("kimi-for-coding"),
		Ignore: map[string]string{
			"k3":                        "Kimi Coding Plan uses the fixed kimi-for-coding model ID",
			"k3-256k":                   "Kimi Coding Plan uses the fixed kimi-for-coding model ID",
			"kimi-for-coding-highspeed": "Kimi Coding Plan uses the fixed kimi-for-coding model ID",
		},
	},
	{Alias: "m", Upstream: "minimax-cn", Prefixes: []string{"MiniMax-"}},
	{
		Alias:    "ds",
		Upstream: "deepseek",
		Prefixes: []string{"deepseek-"},
		Ignore: map[string]string{
			"deepseek-chat":     "retired by DeepSeek on 2026-07-24",
			"deepseek-reasoner": "retired by DeepSeek on 2026-07-24",
		},
	},
	{Alias: "nv", Upstream: "nvidia", Prefixes: []string{"nvidia/", "openai/gpt-oss-"}},
	{
		Alias:    "sf",
		Upstream: "siliconflow-cn",
		Prefixes: []string{
			"deepseek-ai/", "Pro/deepseek-ai/",
			"zai-org/", "Pro/zai-org/",
			"moonshotai/", "Pro/moonshotai/",
			"Qwen/", "Pro/MiniMaxAI/",
		},
		KnownMissing: map[string]string{
			"moonshotai/Kimi-K2.7-Code": "listed by SiliconFlow but not yet present in models.dev",
		},
	},
	{
		Alias:    "q",
		Upstream: "alibaba-cn",
		Prefixes: []string{"qwen"},
		KnownMissing: map[string]string{
			"qwen3-coder-next": "verified in Alibaba documentation but absent from models.dev pay-as-you-go data",
		},
	},
	{
		Alias:    "qc",
		Upstream: "alibaba-coding-plan-cn",
		IDs: stringSet(
			"qwen3.7-plus", "qwen3.6-plus", "kimi-k2.5", "glm-5",
			"MiniMax-M2.5", "qwen3.5-plus", "qwen3-max-2026-01-23",
			"qwen3-coder-next", "qwen3-coder-plus", "glm-4.7",
		),
		Ignore: map[string]string{
			"qwen3.7-max":   "not supported by Alibaba Coding Plan",
			"qwen3.6-flash": "not supported by Alibaba Coding Plan",
		},
	},
	{
		Alias:    "or",
		Upstream: "openrouter",
		Prefixes: []string{
			"anthropic/", "openai/", "qwen/", "z-ai/",
			"moonshotai/", "minimax/", "deepseek/", "google/",
		},
	},
}

type candidate struct {
	ID          string
	ReleaseDate string
	LastUpdated string
}

type routeReport struct {
	Alias          string
	Upstream       string
	CurrentCount   int
	MatchedCount   int
	Candidates     []candidate
	KnownMissing   map[string]string
	UnknownMissing []string
}

type auditReport struct {
	Revision string
	Routes   []routeReport
	Unmapped []string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("catalog-audit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	source := flags.String("source", defaultSource, "models.dev API URL or a local JSON file")
	catalogPath := flags.String("catalog", "catalog-v2.json", "MuxLM v2 catalog file")
	limit := flags.Int("limit", 8, "maximum candidates shown per route (0 shows all)")
	strict := flags.Bool("strict", false, "fail when an unacknowledged current model is missing upstream")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *limit < 0 {
		fmt.Fprintln(stderr, "--limit must be zero or greater")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var catalog muxCatalog
	if err := decodeSource(ctx, *catalogPath, &catalog); err != nil {
		fmt.Fprintf(stderr, "catalog: %v\n", err)
		return 1
	}
	var upstream map[string]upstreamProvider
	if err := decodeSource(ctx, *source, &upstream); err != nil {
		fmt.Fprintf(stderr, "source: %v\n", err)
		return 1
	}

	report := auditCatalog(catalog, upstream)
	printReport(stdout, report, *limit)
	if *strict && report.hasUnknownMissing() {
		return 1
	}
	return 0
}

func decodeSource(ctx context.Context, source string, target any) error {
	data, err := readSource(ctx, source)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("invalid JSON: multiple values")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func readSource(ctx context.Context, source string) ([]byte, error) {
	parsed, err := url.Parse(source)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" {
		return readLimitedFile(source)
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("URL must use HTTPS: %s", source)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "MuxLM-catalog-audit")
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("refusing redirect to a non-HTTPS URL")
			}
			return nil
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", response.Status)
	}
	return readLimited(response.Body)
}

func readLimitedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readLimited(file)
}

func readLimited(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxSourceSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSourceSize {
		return nil, fmt.Errorf("JSON exceeds %d bytes", maxSourceSize)
	}
	return data, nil
}

func auditCatalog(catalog muxCatalog, upstream map[string]upstreamProvider) auditReport {
	providers := make(map[string]muxProvider, len(catalog.Providers))
	for _, provider := range catalog.Providers {
		providers[provider.Alias] = provider
	}

	report := auditReport{Revision: catalog.Revision}
	mapped := make(map[string]bool, len(sourceRules))
	for _, rule := range sourceRules {
		provider, exists := providers[rule.Alias]
		if !exists {
			continue
		}
		mapped[rule.Alias] = true
		upstreamProvider := upstream[rule.Upstream]
		route := auditRoute(provider, upstreamProvider, rule)
		report.Routes = append(report.Routes, route)
	}
	for _, provider := range catalog.Providers {
		if !mapped[provider.Alias] {
			report.Unmapped = append(report.Unmapped, provider.Alias)
		}
	}
	sort.Strings(report.Unmapped)
	return report
}

func auditRoute(provider muxProvider, upstream upstreamProvider, rule sourceRule) routeReport {
	route := routeReport{
		Alias:        provider.Alias,
		Upstream:     rule.Upstream,
		CurrentCount: len(provider.Models),
		KnownMissing: make(map[string]string),
	}
	current := make(map[string]bool, len(provider.Models))
	for _, model := range provider.Models {
		current[model.ID] = true
		if _, exists := upstream.Models[model.ID]; exists {
			route.MatchedCount++
			continue
		}
		if reason, known := rule.KnownMissing[model.ID]; known {
			route.KnownMissing[model.ID] = reason
			continue
		}
		route.UnknownMissing = append(route.UnknownMissing, model.ID)
	}
	sort.Strings(route.UnknownMissing)

	for id, model := range upstream.Models {
		if current[id] || !eligibleCandidate(id, model, rule) {
			continue
		}
		route.Candidates = append(route.Candidates, candidate{
			ID:          id,
			ReleaseDate: model.ReleaseDate,
			LastUpdated: model.LastUpdated,
		})
	}
	sort.Slice(route.Candidates, func(i, j int) bool {
		left, right := route.Candidates[i], route.Candidates[j]
		if left.ReleaseDate != right.ReleaseDate {
			return left.ReleaseDate > right.ReleaseDate
		}
		if left.LastUpdated != right.LastUpdated {
			return left.LastUpdated > right.LastUpdated
		}
		return left.ID < right.ID
	})
	return route
}

func eligibleCandidate(id string, model upstreamModel, rule sourceRule) bool {
	if _, ignored := rule.Ignore[id]; ignored {
		return false
	}
	if len(rule.IDs) > 0 && !rule.IDs[id] {
		return false
	}
	if len(rule.Prefixes) > 0 && !hasAnyPrefix(id, rule.Prefixes) {
		return false
	}
	if strings.EqualFold(model.Status, "deprecated") || !model.ToolCall {
		return false
	}
	if len(model.Modalities.Output) > 0 && !contains(model.Modalities.Output, "text") {
		return false
	}
	return true
}

func printReport(writer io.Writer, report auditReport, limit int) {
	fmt.Fprintf(writer, "MuxLM catalog %s compared with models.dev\n", report.Revision)
	for _, route := range report.Routes {
		fmt.Fprintf(writer, "\n%s <- %s\n", route.Alias, route.Upstream)
		fmt.Fprintf(writer, "  current: %d/%d found upstream\n", route.MatchedCount, route.CurrentCount)
		knownIDs := sortedKeys(route.KnownMissing)
		for _, id := range knownIDs {
			fmt.Fprintf(writer, "  noted: %s — %s\n", id, route.KnownMissing[id])
		}
		for _, id := range route.UnknownMissing {
			fmt.Fprintf(writer, "  missing: %s\n", id)
		}
		shown := len(route.Candidates)
		if limit > 0 && shown > limit {
			shown = limit
		}
		if len(route.Candidates) == 0 {
			fmt.Fprintln(writer, "  candidates: none")
			continue
		}
		fmt.Fprintf(writer, "  candidates: %d", len(route.Candidates))
		if shown < len(route.Candidates) {
			fmt.Fprintf(writer, " (showing %d)", shown)
		}
		fmt.Fprintln(writer)
		for _, model := range route.Candidates[:shown] {
			date := model.ReleaseDate
			if date == "" {
				date = model.LastUpdated
			}
			if date == "" {
				date = "date unknown"
			}
			fmt.Fprintf(writer, "    %s  %s\n", model.ID, date)
		}
	}
	if len(report.Unmapped) > 0 {
		fmt.Fprintf(writer, "\nNo models.dev mapping: %s\n", strings.Join(report.Unmapped, ", "))
	}
}

func (report auditReport) hasUnknownMissing() bool {
	for _, route := range report.Routes {
		if len(route.UnknownMissing) > 0 {
			return true
		}
	}
	return false
}

func stringSet(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
