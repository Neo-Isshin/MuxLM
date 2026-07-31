package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditFiltersCandidatesAndSortsNewestFirst(t *testing.T) {
	catalog := muxCatalog{
		Revision: "2026-07-30.1",
		Providers: []muxProvider{{
			Alias:  "glm",
			Models: []muxModel{{ID: "glm-5.2"}},
		}},
	}
	upstream := map[string]upstreamProvider{
		"zhipuai": {
			Models: map[string]upstreamModel{
				"glm-5.2": {
					ToolCall:   true,
					Modalities: modalities{Output: []string{"text"}},
				},
				"glm-5.3": {
					ToolCall:    true,
					ReleaseDate: "2026-08-01",
					Modalities:  modalities{Output: []string{"text"}},
				},
				"glm-5.1": {
					ToolCall:    true,
					ReleaseDate: "2026-03-01",
					Modalities:  modalities{Output: []string{"text"}},
				},
				"glm-image-only": {
					ToolCall:   true,
					Modalities: modalities{Output: []string{"image"}},
				},
				"glm-no-tools": {
					ToolCall:   false,
					Modalities: modalities{Output: []string{"text"}},
				},
				"glm-retired": {
					ToolCall:   true,
					Status:     "deprecated",
					Modalities: modalities{Output: []string{"text"}},
				},
			},
		},
	}

	report := auditCatalog(catalog, upstream)
	if report.hasUnknownMissing() {
		t.Fatalf("unexpected missing model: %#v", report)
	}
	if len(report.Routes) != 1 {
		t.Fatalf("routes = %d", len(report.Routes))
	}
	candidates := report.Routes[0].Candidates
	if len(candidates) != 2 || candidates[0].ID != "glm-5.3" || candidates[1].ID != "glm-5.1" {
		t.Fatalf("candidates = %#v", candidates)
	}
}

func TestAuditSeparatesKnownAndUnknownMissingModels(t *testing.T) {
	catalog := muxCatalog{
		Revision: "2026-07-30.1",
		Providers: []muxProvider{{
			Alias: "sf",
			Models: []muxModel{
				{ID: "moonshotai/Kimi-K2.7-Code"},
				{ID: "unverified/model"},
			},
		}},
	}

	report := auditCatalog(catalog, map[string]upstreamProvider{
		"siliconflow-cn": {Models: map[string]upstreamModel{}},
	})
	route := report.Routes[0]
	if _, exists := route.KnownMissing["moonshotai/Kimi-K2.7-Code"]; !exists {
		t.Fatalf("known exception missing: %#v", route)
	}
	if len(route.UnknownMissing) != 1 || route.UnknownMissing[0] != "unverified/model" {
		t.Fatalf("unknown missing = %#v", route.UnknownMissing)
	}
	if !report.hasUnknownMissing() {
		t.Fatal("strict audit should fail for an unknown missing model")
	}
}

func TestAuditMissingUpstreamProviderDoesNotDuplicateModels(t *testing.T) {
	catalog := muxCatalog{
		Providers: []muxProvider{{
			Alias:  "glm",
			Models: []muxModel{{ID: "glm-5.2"}},
		}},
	}
	report := auditCatalog(catalog, map[string]upstreamProvider{})
	missing := report.Routes[0].UnknownMissing
	if len(missing) != 1 || missing[0] != "glm-5.2" {
		t.Fatalf("missing models = %#v", missing)
	}
}

func TestKimiCodingAndCodingPlanExceptionsDoNotBecomeCandidates(t *testing.T) {
	kcRule := ruleForAlias(t, "kc")
	kcCatalog := muxCatalog{
		Providers: []muxProvider{{
			Alias:  "kc",
			Models: []muxModel{{ID: "kimi-for-coding"}},
		}},
	}
	kcUpstream := map[string]upstreamProvider{
		kcRule.Upstream: {
			Models: map[string]upstreamModel{
				"k3":                        toolModel(),
				"k3-256k":                   toolModel(),
				"kimi-for-coding":           toolModel(),
				"kimi-for-coding-highspeed": toolModel(),
			},
		},
	}
	if candidates := auditCatalog(kcCatalog, kcUpstream).Routes[0].Candidates; len(candidates) != 0 {
		t.Fatalf("Kimi Coding candidates = %#v", candidates)
	}

	qcRule := ruleForAlias(t, "qc")
	qcCatalog := muxCatalog{
		Providers: []muxProvider{{
			Alias:  "qc",
			Models: []muxModel{{ID: "qwen3.7-plus"}},
		}},
	}
	qcUpstream := map[string]upstreamProvider{
		qcRule.Upstream: {
			Models: map[string]upstreamModel{
				"qwen3.7-plus":  toolModel(),
				"qwen3.7-max":   toolModel(),
				"qwen3.6-flash": toolModel(),
				"unknown-model": toolModel(),
			},
		},
	}
	if candidates := auditCatalog(qcCatalog, qcUpstream).Routes[0].Candidates; len(candidates) != 0 {
		t.Fatalf("Coding Plan candidates = %#v", candidates)
	}
}

func TestRunReadsLocalFixturesAndHonorsStrictMode(t *testing.T) {
	temp := t.TempDir()
	catalogPath := filepath.Join(temp, "catalog.json")
	sourcePath := filepath.Join(temp, "source.json")
	if err := os.WriteFile(catalogPath, []byte(`{
		"revision":"2026-07-30.1",
		"providers":[{"alias":"glm","models":[{"id":"glm-missing"}]}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(`{
		"zhipuai":{"models":{}}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--catalog", catalogPath,
		"--source", sourcePath,
		"--strict",
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "missing: glm-missing") {
		t.Fatalf("output = %s", stdout.String())
	}
}

func ruleForAlias(t *testing.T, alias string) sourceRule {
	t.Helper()
	for _, rule := range sourceRules {
		if rule.Alias == alias {
			return rule
		}
	}
	t.Fatalf("rule %q not found", alias)
	return sourceRule{}
}

func toolModel() upstreamModel {
	return upstreamModel{
		ToolCall:   true,
		Modalities: modalities{Output: []string{"text"}},
	}
}
