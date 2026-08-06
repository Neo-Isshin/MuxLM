package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	languageAuto = "auto"
	languageEN   = "en"
	languageZH   = "zh"
)

type userSettings struct {
	Version  int    `json:"version"`
	Language string `json:"language"`
}

var languageCache struct {
	sync.RWMutex
	value string
}

func settingsFile() string { return filepath.Join(configDir(), "settings.json") }

func tr(zh, en string) string {
	if uiLanguage() == languageZH {
		return zh
	}
	return en
}

func localizedProviderName(name string) string {
	if uiLanguage() == languageZH {
		return name
	}
	translations := map[string]string{
		"智谱 GLM（按量计费 API）":        "Zhipu GLM (pay-as-you-go API)",
		"智谱 GLM Coding Plan（订阅）":  "Zhipu GLM Coding Plan (subscription)",
		"Moonshot Kimi（按量计费 API）": "Moonshot Kimi (pay-as-you-go API)",
		"Kimi for Coding（订阅）":     "Kimi for Coding (subscription)",
		"火山方舟 Coding Plan（订阅）":    "Volcengine Ark Coding Plan (subscription)",
		"阿里云百炼（按量计费 API）":         "Alibaba Cloud Model Studio (pay-as-you-go API)",
		"阿里云百炼 Coding Plan（订阅）":   "Alibaba Cloud Model Studio Coding Plan (subscription)",
		"SiliconFlow 硅基流动":        "SiliconFlow",
	}
	if translated := translations[name]; translated != "" {
		return translated
	}
	return name
}

func uiLanguage() string {
	languageCache.RLock()
	value := languageCache.value
	languageCache.RUnlock()
	if value != "" {
		return value
	}
	value = resolveUILanguage()
	languageCache.Lock()
	if languageCache.value == "" {
		languageCache.value = value
	} else {
		value = languageCache.value
	}
	languageCache.Unlock()
	return value
}

func resolveUILanguage() string {
	preference := normalizeLanguagePreference(os.Getenv("MUXLM_LANG"))
	if preference == "" {
		preference = loadLanguagePreference()
	}
	if preference == languageZH || preference == languageEN {
		return preference
	}
	return detectSystemLanguage()
}

func loadLanguagePreference() string {
	b, err := readPrivateFile(settingsFile())
	if err != nil {
		return languageAuto
	}
	var settings userSettings
	if json.Unmarshal(b, &settings) != nil || settings.Version != 1 {
		return languageAuto
	}
	if preference := normalizeLanguagePreference(settings.Language); preference != "" {
		return preference
	}
	return languageAuto
}

func saveLanguagePreference(preference string) error {
	preference = normalizeLanguagePreference(preference)
	if preference == "" {
		preference = languageAuto
	}
	if err := atomicWriteJSON(settingsFile(), userSettings{Version: 1, Language: preference}); err != nil {
		return err
	}
	reloadUILanguage()
	return nil
}

func normalizeLanguagePreference(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "system", "default":
		return languageAuto
	case "zh", "zh-cn", "zh_cn", "zh-hans", "chinese", "中文":
		return languageZH
	case "en", "en-us", "en_us", "english":
		return languageEN
	default:
		return ""
	}
}

func detectSystemLanguage() string {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		raw := os.Getenv(name)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if value := languageFromLocale(raw); value != "" {
			return value
		}
		return languageEN
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
			if value := languageFromLocale(string(out)); value != "" {
				return value
			}
		}
	}
	return languageEN
}

func languageFromLocale(locale string) string {
	value := strings.ToLower(strings.TrimSpace(locale))
	value = strings.TrimLeft(value, "(\"' \t\r\n")
	switch {
	case strings.HasPrefix(value, "zh"):
		return languageZH
	case strings.HasPrefix(value, "en"):
		return languageEN
	default:
		return ""
	}
}

func reloadUILanguage() {
	languageCache.Lock()
	languageCache.value = ""
	languageCache.Unlock()
}
