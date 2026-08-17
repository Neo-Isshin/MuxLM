package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRetiredCatalogTagCannotBeReusedOrClaimedByCustomProvider(t *testing.T) {
	isolatedConfig(t)
	retired := cloneCatalog(t, &embeddedCatalog)
	retired.Revision = "2099-01-01.1"
	retired.RetiredTags["glm47"] = catalogTagTrustIndex(&embeddedCatalog)["glm47"]
	removeModelWithTag(t, retired, "glm47")
	body, err := json.Marshal(retired)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()
	t.Setenv("PROVIDERDECK_CATALOG_URL", server.URL)

	if _, err := checkCatalogUpdate(context.Background(), false); err != nil {
		t.Fatalf("retirement update failed: %v", err)
	}
	state := loadCatalogUpdateState()
	if !state.RetiredTags["glm47"] || state.TagTargets["glm47"] == "" {
		t.Fatalf("retired tag history was not persisted: %#v", state)
	}

	reused := cloneCatalog(t, retired)
	reused.Revision = "2099-01-01.2"
	reused.Providers[0].Models = append(reused.Providers[0].Models, Model{ID: "different-model", Tag: "glm47", Source: "official"})
	body, err = json.Marshal(reused)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checkCatalogUpdate(context.Background(), false); err == nil || !strings.Contains(err.Error(), "glm47") {
		t.Fatalf("retired tag was reused: %v", err)
	}

	custom := Provider{
		ID:        "custom-glm47",
		Alias:     "glm47",
		Name:      "Custom",
		Plan:      "custom",
		OpenAIURL: "https://example.com",
		KeyEnv:    "PROVIDERDECK_GLM47_KEY",
		CLI:       []string{"codex", "opencode"},
		Models:    []Model{{ID: "custom-model", Latest: true}},
	}
	if err := atomicWriteJSON(customProviderPath(custom.ID), customProviderFile{Version: 1, Provider: custom}); err != nil {
		t.Fatal(err)
	}
	if _, exists := buildIndex()["glm47"]; exists {
		t.Fatal("retired version alias unexpectedly activated a custom route")
	}
}

func TestEmbeddedRetiredTagsAndOfficialM3ShortSurviveFreshInstall(t *testing.T) {
	isolatedConfig(t)
	if target := embeddedCatalog.RetiredTags["m3"]; target != "minimax/standard/MiniMax-M3" {
		t.Fatalf("embedded m3 tombstone = %q", target)
	}
	officialM3, exists := buildIndex()["m3"]
	if !exists || officialM3.Prov.Alias != "m" || officialM3.Model.ID != "MiniMax-M3" {
		t.Fatalf("official m3 short name = %#v", officialM3)
	}

	reused := cloneCatalog(t, &embeddedCatalog)
	reused.Revision = "2099-01-01.1"
	delete(reused.RetiredTags, "doubao-code")
	reused.Providers[0].Models = append(reused.Providers[0].Models, Model{ID: "different-model", Tag: "doubao-code", Source: "official"})
	if err := validateCatalog(reused); err != nil {
		t.Fatal(err)
	}
	if err := validateCatalogEvolution(&embeddedCatalog, reused); err == nil || !strings.Contains(err.Error(), "tombstone") {
		t.Fatalf("embedded retired tag was reusable: %v", err)
	}

	custom := Provider{
		ID:        "custom-m3",
		Alias:     "m3",
		Name:      "Custom M3",
		Plan:      "custom",
		OpenAIURL: "https://example.com",
		KeyEnv:    "PROVIDERDECK_M3_KEY",
		CLI:       []string{"codex", "opencode"},
		Models:    []Model{{ID: "custom-model", Latest: true}},
	}
	if err := atomicWriteJSON(customProviderPath(custom.ID), customProviderFile{Version: 1, Provider: custom}); err != nil {
		t.Fatal(err)
	}
	resolved := buildIndex()["m3"]
	if resolved.Prov == nil || resolved.Prov.Alias != "m" || resolved.Model.ID != "MiniMax-M3" {
		t.Fatalf("custom provider claimed official m3 short: %#v", resolved)
	}
}

func TestCatalogStateV2MigrationPreservesVersionTagHistory(t *testing.T) {
	isolatedConfig(t)
	legacy := catalogUpdateState{
		Version:     2,
		TagTargets:  map[string]string{"oldtag": "provider/standard/model"},
		RetiredTags: map[string]bool{"oldtag": true},
	}
	if err := atomicWriteJSON(updateStateFile(), legacy); err != nil {
		t.Fatal(err)
	}
	migrated := loadCatalogUpdateState()
	if migrated.Version != catalogStateVersion ||
		migrated.TagTargets["oldtag"] != legacy.TagTargets["oldtag"] ||
		!migrated.RetiredTags["oldtag"] {
		t.Fatalf("v2 state history was lost: %#v", migrated)
	}
}

func TestRetiredKimiVersionAliasesRemainTombstoned(t *testing.T) {
	isolatedConfig(t)
	want := map[string]string{
		"k3":     "kimi/coding/k3",
		"kimic":  "kimi/coding/k3",
		"kimi":   "kimi/standard/kimi-k2.6",
		"kimi26": "kimi/standard/kimi-k2.6",
	}
	for alias, target := range want {
		if got := embeddedCatalog.RetiredTags[alias]; got != target {
			t.Fatalf("%s tombstone = %q, want %q", alias, got, target)
		}
		if alias == "k3" {
			resolved, exists := buildIndex()[alias]
			if !exists || resolved.Prov.Alias != "k" || resolved.Model.ID != "kimi-k3" {
				t.Fatalf("official k3 short name = %#v", resolved)
			}
			continue
		}
		if _, exists := buildIndex()[alias]; exists {
			t.Fatalf("retired Kimi alias %q is active", alias)
		}
	}
}

func TestCatalogGrowthFromPreviousRevisionsIsAccepted(t *testing.T) {
	august17 := catalogAtAugust17Revision1(t)
	if err := validateCatalog(august17); err != nil {
		t.Fatalf("August 17.1 catalog fixture is invalid: %v", err)
	}
	if err := validateCatalogEvolution(august17, &embeddedCatalog); err != nil {
		t.Fatalf("August 17.1 release cannot accept the expanded catalog: %v", err)
	}

	july30 := catalogAtJuly30(t)
	if err := validateCatalog(july30); err != nil {
		t.Fatalf("July 30 catalog fixture is invalid: %v", err)
	}
	if err := validateCatalogEvolution(july30, &embeddedCatalog); err != nil {
		t.Fatalf("July 30 release cannot accept the expanded catalog: %v", err)
	}

	july24 := cloneCatalog(t, july30)
	july24.Revision = "2026-07-24.1"
	for _, tag := range []string{
		"glm5v", "glm5", "glm47fx", "glm47f",
		"glmc5v", "glmc51", "glmc5t", "glmc47",
		"k27h",
		"m25std", "m25", "m21", "m2",
		"sfglm52", "sfk26",
		"q36", "q36f", "q35f",
		"oro5", "oro5f", "orq37f",
	} {
		removeModelWithTag(t, july24, tag)
	}
	if err := validateCatalog(july24); err != nil {
		t.Fatalf("July 24 catalog fixture is invalid: %v", err)
	}
	if err := validateCatalogEvolution(july24, &embeddedCatalog); err != nil {
		t.Fatalf("July 24 release cannot accept the expanded catalog: %v", err)
	}
}

func catalogAtAugust17Revision1(t *testing.T) *CatalogFile {
	t.Helper()
	previous := cloneCatalog(t, &embeddedCatalog)
	previous.Revision = "2026-08-17.1"
	removeModelWithTag(t, previous, "glm53")
	for providerIndex := range previous.Providers {
		provider := &previous.Providers[providerIndex]
		if provider.Alias != "glm" {
			continue
		}
		for modelIndex := range provider.Models {
			if provider.Models[modelIndex].ID == "glm-5.2" {
				provider.Models[modelIndex].Latest = true
			}
		}
	}
	return previous
}

func catalogAtJuly30(t *testing.T) *CatalogFile {
	t.Helper()
	previous := catalogAtAugust17Revision1(t)
	previous.Revision = "2026-07-30.1"
	for _, tag := range []string{
		"glmc53", "nvn35l",
		"ordsv4p", "ordsv4f", "orq38m", "orq3827", "orq3824t", "orgem37f", "orn35l",
	} {
		removeModelWithTag(t, previous, tag)
	}
	for providerIndex := range previous.Providers {
		provider := &previous.Providers[providerIndex]
		for modelIndex := range provider.Models {
			model := &provider.Models[modelIndex]
			switch {
			case provider.Alias == "glmc" && model.ID == "glm-5.2":
				model.Tag = ""
				model.Latest = true
			case provider.Alias == "nv" && model.ID == "openai/gpt-oss-120b":
				model.Latest = true
			}
		}
	}
	return previous
}

func removeModelWithTag(t *testing.T, catalog *CatalogFile, tag string) {
	t.Helper()
	for providerIndex := range catalog.Providers {
		models := catalog.Providers[providerIndex].Models
		for modelIndex := range models {
			if models[modelIndex].Tag == tag {
				catalog.Providers[providerIndex].Models = append(models[:modelIndex:modelIndex], models[modelIndex+1:]...)
				return
			}
		}
	}
	t.Fatalf("tag %q not found", tag)
}
