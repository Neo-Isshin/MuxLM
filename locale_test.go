package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSystemLanguageDetection(t *testing.T) {
	tests := []struct {
		name, locale, want string
	}{
		{"Simplified Chinese", "zh_CN.UTF-8", languageZH},
		{"Traditional Chinese", "zh_TW.UTF-8", languageZH},
		{"English", "en_US.UTF-8", languageEN},
		{"Unsupported defaults to English", "fr_FR.UTF-8", languageEN},
		{"C locale defaults to English", "C.UTF-8", languageEN},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MUXLM_LANG", "")
			t.Setenv("LC_ALL", tc.locale)
			t.Setenv("LC_MESSAGES", "zh_CN.UTF-8")
			t.Setenv("LANG", "zh_CN.UTF-8")
			reloadUILanguage()
			t.Cleanup(reloadUILanguage)
			if got := uiLanguage(); got != tc.want {
				t.Fatalf("language = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLanguagePreferencePersistsAndOverridesSystem(t *testing.T) {
	root := isolatedConfig(t)
	t.Setenv("MUXLM_LANG", "")
	t.Setenv("LC_ALL", "fr_FR.UTF-8")
	reloadUILanguage()

	withStdin(t, "3\n", func() {
		if err := configureLanguage(); err != nil {
			t.Fatal(err)
		}
	})
	if got := uiLanguage(); got != languageZH {
		t.Fatalf("saved language = %q", got)
	}
	b, err := os.ReadFile(settingsFile())
	if err != nil {
		t.Fatal(err)
	}
	var settings userSettings
	if err := json.Unmarshal(b, &settings); err != nil || settings.Language != languageZH {
		t.Fatalf("settings = %#v, err=%v, root=%s", settings, err, root)
	}

	withStdin(t, "1\n", func() {
		if err := configureLanguage(); err != nil {
			t.Fatal(err)
		}
	})
	if got := uiLanguage(); got != languageEN {
		t.Fatalf("unsupported system language did not fall back to English: %q", got)
	}
}

func TestLanguageEnvironmentOverridesSavedPreference(t *testing.T) {
	isolatedConfig(t)
	if err := saveLanguagePreference(languageZH); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUXLM_LANG", "en")
	reloadUILanguage()
	t.Cleanup(reloadUILanguage)
	if got := uiLanguage(); got != languageEN {
		t.Fatalf("environment override = %q", got)
	}
}

func TestEnglishCoreOutput(t *testing.T) {
	isolatedConfig(t)
	t.Setenv("MUXLM_LANG", "en")
	reloadUILanguage()
	t.Cleanup(reloadUILanguage)

	if text := helpText(); !strings.Contains(text, "Usage:") || strings.Contains(text, "用法:") {
		t.Fatalf("help is not English:\n%s", text)
	}
	list := captureStdout(t, printTable)
	if !strings.Contains(list, "Native account / config") || strings.Contains(list, "原生账号") {
		t.Fatalf("list is not English:\n%s", list)
	}
	preview := captureStdout(t, func() { previewDefault("claude", false, nil) })
	if !strings.Contains(preview, "native account / default model") {
		t.Fatalf("def preview is not English:\n%s", preview)
	}
	config := captureStdout(t, func() {
		if err := printConfig("claude"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(config, "Interface language: en") {
		t.Fatalf("config is not English:\n%s", config)
	}
}

func TestClaudeConfigMenuOffersLanguageChoice(t *testing.T) {
	isolatedConfig(t)
	claudeMenu := captureStderr(t, func() {
		withStdin(t, "0\n", func() {
			if err := runConfigMenu("claude"); err != nil {
				t.Fatal(err)
			}
		})
	})
	if !strings.Contains(claudeMenu, "6) Language / 语言") {
		t.Fatalf("cld config menu has no language choice:\n%s", claudeMenu)
	}
	codexMenu := captureStderr(t, func() {
		withStdin(t, "0\n", func() {
			if err := runConfigMenu("codex"); err != nil {
				t.Fatal(err)
			}
		})
	})
	if strings.Contains(codexMenu, "Language / 语言") {
		t.Fatalf("language choice unexpectedly appeared outside cld config:\n%s", codexMenu)
	}
}
