package main

import "os"

const (
	appName                         = "MuxLM"
	binaryName                      = "muxlm"
	secretService                   = "muxlm"
	legacyProviderDeckSecretService = "providerdeck"
	legacyEZSwitchSecretService     = "ez-switch"
)

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func secretServicesForRead() []string {
	return []string{secretService, legacyProviderDeckSecretService, legacyEZSwitchSecretService}
}
