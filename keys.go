package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/term"
)

type KeyRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Plan    string `json:"plan"`
	Region  string `json:"region"`
	Backend string `json:"backend"`
	Ref     string `json:"secret_ref"`
}

type keyFile struct {
	Version int         `json:"version"`
	Keys    []KeyRecord `json:"keys"`
}

func keysFile() string                  { return filepath.Join(configDir(), "keys.env") } // v1 兼容读取
func providerKeysFile(id string) string { return filepath.Join(providerDir(id), "keys.json") }

func loadLegacyKeys() map[string]string {
	k := make(map[string]string)
	if data, err := readPrivateFile(keysFile()); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if eq := strings.IndexByte(line, '='); eq > 0 {
				// 旧文件可能有重复项；最后一项应当生效，便于 key 轮换。
				k[strings.TrimSpace(line[:eq])] = strings.TrimSpace(line[eq+1:])
			}
		}
	}
	return k
}

func loadProviderKeys(id string) ([]KeyRecord, error) {
	path := providerKeysFile(id)
	b, err := readPrivateFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f keyFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s 损坏: %w", path, err)
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("%s 使用不支持的版本: %d", path, f.Version)
	}
	if err := validateKeyRecords(id, f.Keys); err != nil {
		return nil, fmt.Errorf("%s 无效: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)
	return f.Keys, nil
}

func validateKeyRecords(providerID string, keys []KeyRecord) error {
	if providerID == "" || safeID(providerID) != providerID {
		return fmt.Errorf("非法 provider id")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, k := range keys {
		if k.ID == "" || safeID(k.ID) != k.ID || ids[k.ID] {
			return fmt.Errorf("非法或重复 key id")
		}
		ids[k.ID] = true
		if k.Plan == "" || safeID(k.Plan) != k.Plan {
			return fmt.Errorf("非法 plan")
		}
		if k.Region != "cn" && k.Region != "intl" {
			return fmt.Errorf("非法 region")
		}
		if k.Backend != "keychain" && k.Backend != "secret-service" && k.Backend != "file" {
			return fmt.Errorf("非法 backend")
		}
		if k.Ref != fmt.Sprintf("provider/%s/key/%s", providerID, k.ID) {
			return fmt.Errorf("非法 secret_ref")
		}
		if k.Name == "" || len(k.Name) > 64 || strings.ContainsAny(k.Name, "\r\n\t") {
			return fmt.Errorf("非法 key 名称")
		}
		nameKey := k.Plan + "/" + k.Region + "/" + k.Name
		if names[nameKey] {
			return fmt.Errorf("重复 key 名称")
		}
		names[nameKey] = true
	}
	return nil
}

func saveProviderKeys(id string, keys []KeyRecord) error {
	return atomicWriteJSON(providerKeysFile(id), keyFile{Version: 1, Keys: keys})
}

type keyCandidate struct {
	Name, Source, Value string
	Record              *KeyRecord
}

func getKey(p *Provider, intl *bool, cli, model string) (string, error) {
	if *intl && !p.hasIntlFor(cli) {
		*intl = false
	}
	cnEnv, intlEnv := p.KeyEnv, ""
	if p.hasIntlFor(cli) {
		intlEnv = p.KeyEnv + "_INTL"
	}
	if !*intl && p.hasIntlFor(cli) && os.Getenv(cnEnv) == "" && loadLegacyKeys()[cnEnv] == "" {
		keys, _ := loadProviderKeys(p.providerID())
		hasCN, hasIntl := false, false
		for _, k := range keys {
			if !keyPlanMatches(p, k.Plan) {
				continue
			}
			if k.Region == "intl" {
				hasIntl = true
			} else {
				hasCN = true
			}
		}
		if !hasCN && hasIntl {
			*intl = true
		}
		if !hasCN && !hasIntl && intlEnv != "" && (os.Getenv(intlEnv) != "" || loadLegacyKeys()[intlEnv] != "") {
			*intl = true
		}
	}
	region := "cn"
	if *intl {
		region = "intl"
	}

	for {
		candidates, err := keyCandidates(p, region)
		if err != nil {
			return "", err
		}
		if len(candidates) == 0 {
			if p.hasIntlFor(cli) && !*intl {
				fmt.Fprintf(os.Stderr, "\n%s 尚未配置 %s key。\n", p.Name, planDisplay(p.planID()))
				*intl = chooseIntl(p, cli)
				if *intl {
					region = "intl"
				}
			}
			return addNamedKey(p, region, cli, model)
		}
		if len(candidates) == 1 {
			return resolveCandidate(p, candidates[0])
		}
		chosen, retry, err := chooseKeyCandidate(p, region, candidates)
		if err != nil {
			return "", err
		}
		if retry {
			continue
		}
		return resolveCandidate(p, chosen)
	}
}

func keyCandidates(p *Provider, region string) ([]keyCandidate, error) {
	var out []keyCandidate
	envName := p.KeyEnv
	if region == "intl" && p.hasIntl() {
		envName += "_INTL"
	}
	if v := os.Getenv(envName); v != "" {
		out = append(out, keyCandidate{Name: envName, Source: "env", Value: v})
	} else if v := loadLegacyKeys()[envName]; v != "" {
		out = append(out, keyCandidate{Name: envName, Source: "legacy-file", Value: v})
	}
	if p.Key != "" {
		out = append(out, keyCandidate{Name: "legacy-custom", Source: "legacy-file", Value: p.Key})
	}
	keys, err := loadProviderKeys(p.providerID())
	if err != nil {
		return nil, err
	}
	for i := range keys {
		k := &keys[i]
		if keyPlanMatches(p, k.Plan) && k.Region == region {
			out = append(out, keyCandidate{Name: k.Name, Source: k.Backend, Record: k})
		}
	}
	return out, nil
}

func resolveCandidate(p *Provider, c keyCandidate) (string, error) {
	if c.Record == nil {
		return c.Value, nil
	}
	return secretGet(p.providerID(), c.Record.Backend, c.Record.Ref)
}

func chooseKeyCandidate(p *Provider, region string, candidates []keyCandidate) (keyCandidate, bool, error) {
	for {
		fmt.Fprintf(os.Stderr, "\n%s 有多个可用 key（%s / %s）:\n", p.Name, planDisplay(p.planID()), regionDisplay(region))
		for i, c := range candidates {
			fmt.Fprintf(os.Stderr, "  %d) %s  [%s]\n", i+1, c.Name, c.Source)
		}
		fmt.Fprint(os.Stderr, "选择 [1]，输入 x 删除已保存 key: ")
		s := strings.ToLower(promptLine(""))
		if s == "" {
			return candidates[0], false, nil
		}
		if s == "x" {
			fmt.Fprint(os.Stderr, "输入要删除的编号（回车取消）: ")
			n, _ := strconv.Atoi(promptLine(""))
			if n < 1 || n > len(candidates) {
				continue
			}
			c := candidates[n-1]
			if c.Record == nil {
				fmt.Fprintln(os.Stderr, "⚠ 环境变量/旧配置不能在此删除")
				continue
			}
			confirm := strings.ToLower(promptLine(fmt.Sprintf("确认删除 key %q？输入 yes: ", c.Name)))
			if confirm != "yes" {
				fmt.Fprintln(os.Stderr, "已取消")
				continue
			}
			if err := deleteKeyRecord(p.providerID(), c.Record.ID); err != nil {
				return keyCandidate{}, false, err
			}
			fmt.Fprintln(os.Stderr, "✓ 已删除")
			return keyCandidate{}, true, nil
		}
		n, _ := strconv.Atoi(s)
		if n >= 1 && n <= len(candidates) {
			return candidates[n-1], false, nil
		}
	}
}

func deleteKeyRecord(providerID, id string) error {
	keys, err := loadProviderKeys(providerID)
	if err != nil {
		return err
	}
	for i, k := range keys {
		if k.ID != id {
			continue
		}
		if err := secretDelete(providerID, k.Backend, k.Ref); err != nil {
			return err
		}
		keys = append(keys[:i], keys[i+1:]...)
		return saveProviderKeys(providerID, keys)
	}
	return fmt.Errorf("key 不存在")
}

func addNamedKey(p *Provider, region, cli, model string) (string, error) {
	keys, err := loadProviderKeys(p.providerID())
	if err != nil {
		return "", err
	}
	var names []string
	for _, k := range keys {
		if keyPlanMatches(p, k.Plan) && k.Region == region {
			names = append(names, k.Name)
		}
	}
	def := nextKeyName(names)
	fmt.Fprintf(os.Stderr, "\n已有 key 名称: %s\n", emptyAs(strings.Join(names, ", "), "(无)"))
	name := promptLine("新 key 名称（回车用 " + def + "）: ")
	if name == "" {
		name = def
	}
	if len(name) > 64 || strings.ContainsAny(name, "\r\n\t") {
		return "", fmt.Errorf("key 名称不合法（最长 64 字符，不能含控制字符）")
	}
	for _, k := range keys {
		if keyPlanMatches(p, k.Plan) && k.Region == region && k.Name == name {
			return "", fmt.Errorf("key 名称 %q 已存在，请换一个名称", name)
		}
	}
	var val string
	intl := region == "intl"
	for {
		val, err = readHiddenPrompt("API key（输入隐藏，回车取消）: ")
		if err != nil {
			return "", err
		}
		if val == "" {
			return "", fmt.Errorf("已取消")
		}
		if p.planID() == "custom" {
			proto, base := keyProbeTarget(p, cli, intl)
			fmt.Fprintln(os.Stderr, "探测自定义端点…")
			reachable, code, msg := probe(proto, base, model, val)
			if !reachable || code < 200 || code >= 300 {
				fmt.Fprintln(os.Stderr, msg)
				fmt.Fprintln(os.Stderr, "↻ 检测未通过，请重新输入 key")
				continue
			}
		} else if note, bad := checkKey(p, cli, model, intl, val); bad {
			fmt.Fprintln(os.Stderr, note)
			fmt.Fprintln(os.Stderr, "↻ key 无效，请重新输入")
			continue
		} else if note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
		break
	}
	id := randomID()
	ref := fmt.Sprintf("provider/%s/key/%s", p.providerID(), id)
	backend, err := secretSet(p.providerID(), ref, val)
	if err != nil {
		return "", err
	}
	rec := KeyRecord{ID: id, Name: name, Plan: p.planID(), Region: region, Backend: backend, Ref: ref}
	keys = append(keys, rec)
	if err := saveProviderKeys(p.providerID(), keys); err != nil {
		_ = secretDelete(p.providerID(), backend, ref)
		return "", err
	}
	if backend == "file" {
		fmt.Fprintln(os.Stderr, "⚠ 系统 keychain 不可用；key 已以明文存入 600 权限文件")
	}
	fmt.Fprintf(os.Stderr, "✓ 已保存 key %q [%s]\n", name, backend)
	return val, nil
}

func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("key-%d", os.Getpid())
	}
	return hex.EncodeToString(b)
}

func planDisplay(s string) string {
	if s == "coding" {
		return "Coding Plan"
	}
	return s
}
func regionDisplay(s string) string {
	if s == "intl" {
		return "海外"
	}
	return "国内"
}
func emptyAs(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func nextKeyName(existing []string) string {
	used := map[string]bool{}
	for _, name := range existing {
		used[name] = true
	}
	for i := 1; ; i++ {
		name := fmt.Sprintf("key%d", i)
		if !used[name] {
			return name
		}
	}
}

func checkKey(p *Provider, cli, model string, intl bool, key string) (note string, badKey bool) {
	proto, base := keyProbeTarget(p, cli, intl)
	if base == "" {
		return "", false
	}
	fmt.Fprintln(os.Stderr, "检测 key…")
	reachable, code, msg := probe(proto, base, model, key)
	switch {
	case reachable && (code == 401 || code == 403):
		return msg, true
	case !reachable:
		return "⚠ 暂时连不上端点，已保存 key（若实际启动失败再核对）", false
	case code < 200 || code >= 300:
		return msg + "（key 鉴权已通过，已保存）", false
	default:
		return "", false
	}
}

// keyPlanMatches keeps v1 Doubao key metadata usable after the catalog moved
// that provider from the pay-as-you-go route to the official Coding Plan.
func keyPlanMatches(p *Provider, storedPlan string) bool {
	if storedPlan == p.planID() {
		return true
	}
	return p.providerID() == "doubao" && p.planID() == "coding" && storedPlan == "standard"
}

func keyProbeTarget(p *Provider, cli string, intl bool) (protocol, base string) {
	protocol, base = p.probeTarget(cli, intl)
	if protocol == "openai" && p.wireAPI() == "responses" {
		protocol = "responses"
	}
	return protocol, base
}

func chooseIntl(p *Provider, cli string) bool {
	fmt.Fprintf(os.Stderr, "选择端点:\n  1) 国内  %s（默认）\n  2) 海外  %s\n", p.hostFor(cli, false), p.hostFor(cli, true))
	s := promptLine("请选择 [1]: ")
	return s == "2"
}

func readHidden() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return readLineCooked(), nil
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
