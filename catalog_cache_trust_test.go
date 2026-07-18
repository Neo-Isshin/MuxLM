package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacyCacheWithChangedEndpointIsNeverTrusted(t *testing.T) {
	isolatedConfig(t)
	malicious := cloneCatalog(t, &embeddedCatalog)
	malicious.Revision = "2099-01-01.1"
	malicious.Providers[0].OpenAIURL = "https://redirected.example"
	if err := atomicWriteJSON(catalogCacheFile(), malicious); err != nil {
		t.Fatal(err)
	}

	remote := cloneCatalog(t, &embeddedCatalog)
	remote.Revision = "2099-01-01.2"
	body, err := json.Marshal(remote)
	if err != nil {
		t.Fatal(err)
	}
	conditional := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conditional = r.Header.Get("If-None-Match")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	t.Setenv("PROVIDERDECK_CATALOG_URL", server.URL)

	// Version 1 represents update state written by the pre-ProviderDeck updater.
	// It must not make an old, trust-sensitive cache authoritative.
	if err := atomicWriteJSON(updateStateFile(), catalogUpdateState{
		Version:       1,
		URL:           server.URL,
		ETag:          `"legacy"`,
		Revision:      malicious.Revision,
		CatalogDigest: catalogDigest(malicious),
	}); err != nil {
		t.Fatal(err)
	}

	if got := activeCatalogRevision(); got != embeddedCatalog.Revision {
		t.Fatalf("untrusted cache became active: %s", got)
	}
	if got := catalogProviders()[0].OpenAIURL; got != embeddedCatalog.Providers[0].OpenAIURL {
		t.Fatalf("untrusted endpoint became active: %s", got)
	}
	if _, err := checkCatalogUpdate(context.Background(), true); err != nil {
		t.Fatalf("safe replacement update failed: %v", err)
	}
	if conditional != "" {
		t.Fatalf("sent conditional validator for untrusted cache: %q", conditional)
	}
	if got := activeCatalogRevision(); got != remote.Revision {
		t.Fatalf("safe remote catalog did not replace cache: %s", got)
	}
}

func TestRuntimeAndDoctorBothRejectChangedContentAtEmbeddedRevision(t *testing.T) {
	isolatedConfig(t)
	changed := cloneCatalog(t, &embeddedCatalog)
	changed.Providers[0].Name += " changed without a revision"
	if err := atomicWriteJSON(catalogCacheFile(), changed); err != nil {
		t.Fatal(err)
	}

	if _, err := loadCachedCatalog(); err == nil || !strings.Contains(err.Error(), "同 revision") {
		t.Fatalf("runtime accepted mutable catalog revision: %v", err)
	}
	status := inspectDoctorCatalog()
	if len(status.errors) != 1 || !strings.Contains(status.errors[0], "同 revision") {
		t.Fatalf("doctor disagreed with runtime: %#v", status)
	}
	if got := activeCatalogRevision(); got != embeddedCatalog.Revision {
		t.Fatalf("changed same-revision cache became active: %s", got)
	}
}
