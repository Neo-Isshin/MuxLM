package main

import "os"

const (
	appName             = "ProviderDeck"
	binaryName          = "providerdeck"
	secretService       = "providerdeck"
	legacySecretService = "ez-switch"
)

func firstEnv(primary, legacy string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	return os.Getenv(legacy)
}
