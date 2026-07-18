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
	reused.Providers[0].Models = append(reused.Providers[0].Models, Model{ID: "different-model", Tag: "glm47"})
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

func TestEmbeddedRetiredTagSurvivesFreshInstall(t *testing.T) {
	isolatedConfig(t)
	if target := embeddedCatalog.RetiredTags["m3"]; target != "minimax/standard/MiniMax-M3" {
		t.Fatalf("embedded m3 tombstone = %q", target)
	}

	reused := cloneCatalog(t, &embeddedCatalog)
	reused.Revision = "2099-01-01.1"
	delete(reused.RetiredTags, "m3")
	reused.Providers[4].Models = append(reused.Providers[4].Models, Model{ID: "different-model", Tag: "m3"})
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
	if _, exists := buildIndex()["m3"]; exists {
		t.Fatal("embedded retired tag unexpectedly activated a custom route")
	}
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
