package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
		return nil, fmt.Errorf(tr("%s 损坏: %w", "%s is corrupt: %w"), path, err)
	}
	if f.Version != 1 {
		return nil, fmt.Errorf(tr("%s 使用不支持的版本: %d", "%s uses unsupported version: %d"), path, f.Version)
	}
	if err := validateKeyRecords(id, f.Keys); err != nil {
		return nil, fmt.Errorf(tr("%s 无效: %w", "%s is invalid: %w"), path, err)
	}
	_ = os.Chmod(path, 0o600)
	return f.Keys, nil
}

func validateKeyRecords(providerID string, keys []KeyRecord) error {
	if providerID == "" || safeID(providerID) != providerID {
		return fmt.Errorf(tr("非法 provider id", "invalid provider ID"))
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, k := range keys {
		if k.ID == "" || safeID(k.ID) != k.ID || ids[k.ID] {
			return fmt.Errorf(tr("非法或重复 key id", "invalid or duplicate key ID"))
		}
		ids[k.ID] = true
		if k.Plan == "" || safeID(k.Plan) != k.Plan {
			return fmt.Errorf(tr("非法 plan", "invalid plan"))
		}
		if k.Region != "cn" && k.Region != "intl" {
			return fmt.Errorf(tr("非法 region", "invalid region"))
		}
		if k.Backend != "keychain" && k.Backend != "secret-service" && k.Backend != "file" {
			return fmt.Errorf(tr("非法 backend", "invalid backend"))
		}
		if k.Ref != fmt.Sprintf("provider/%s/key/%s", providerID, k.ID) {
			return fmt.Errorf(tr("非法 secret_ref", "invalid secret_ref"))
		}
		if k.Name == "" || len(k.Name) > 64 || strings.ContainsAny(k.Name, "\r\n\t") {
			return fmt.Errorf(tr("非法 key 名称", "invalid key name"))
		}
		nameKey := k.Plan + "/" + k.Region + "/" + k.Name
		if names[nameKey] {
			return fmt.Errorf(tr("重复 key 名称", "duplicate key name"))
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
				fmt.Fprintf(os.Stderr, tr("\n%s 尚未配置 %s key。\n", "\n%s has no configured %s key.\n"), localizedProviderName(p.Name), planDisplay(p.planID()))
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
		fmt.Fprintf(os.Stderr, tr("\n%s 有多个可用 key（%s / %s）:\n", "\n%s has multiple available keys (%s / %s):\n"), localizedProviderName(p.Name), planDisplay(p.planID()), regionDisplay(region))
		for i, c := range candidates {
			fmt.Fprintf(os.Stderr, "  %d) %s  [%s]\n", i+1, c.Name, c.Source)
		}
		fmt.Fprint(os.Stderr, tr("选择 [1]，输入 x 删除已保存 key: ", "Choose [1], or enter x to delete a saved key: "))
		s := strings.ToLower(promptLine(""))
		if s == "" {
			return candidates[0], false, nil
		}
		if s == "x" {
			fmt.Fprint(os.Stderr, tr("输入要删除的编号（回车取消）: ", "Number to delete (Enter to cancel): "))
			n, _ := strconv.Atoi(promptLine(""))
			if n < 1 || n > len(candidates) {
				continue
			}
			c := candidates[n-1]
			if c.Record == nil {
				fmt.Fprintln(os.Stderr, tr("⚠ 环境变量/旧配置不能在此删除", "⚠ Environment and legacy configuration keys cannot be deleted here"))
				continue
			}
			confirm := strings.ToLower(promptLine(fmt.Sprintf(tr("确认删除 key %q？输入 yes: ", "Delete key %q? Enter yes: "), c.Name)))
			if confirm != "yes" {
				fmt.Fprintln(os.Stderr, tr("已取消", "Cancelled"))
				continue
			}
			if err := deleteKeyRecord(p.providerID(), c.Record.ID); err != nil {
				return keyCandidate{}, false, err
			}
			fmt.Fprintln(os.Stderr, tr("✓ 已删除", "✓ Deleted"))
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
	return fmt.Errorf(tr("key 不存在", "key does not exist"))
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
	fmt.Fprintf(os.Stderr, tr("\n已有 key 名称: %s\n", "\nExisting key names: %s\n"), emptyAs(strings.Join(names, ", "), tr("(无)", "(none)")))
	name := promptLine(tr("新 key 名称（回车用 ", "New key name (Enter for ") + def + tr("）: ", "): "))
	if name == "" {
		name = def
	}
	if len(name) > 64 || strings.ContainsAny(name, "\r\n\t") {
		return "", fmt.Errorf(tr("key 名称不合法（最长 64 字符，不能含控制字符）", "invalid key name (maximum 64 characters; no control characters)"))
	}
	for _, k := range keys {
		if keyPlanMatches(p, k.Plan) && k.Region == region && k.Name == name {
			return "", fmt.Errorf(tr("key 名称 %q 已存在，请换一个名称", "key name %q already exists; choose another name"), name)
		}
	}
	backendChoice := chooseSecretBackend()
	if runtime.GOOS == "linux" && backendChoice.name == "file" && !backendChoice.explicit {
		reason := strings.TrimSpace(backendChoice.reason)
		if reason == "" {
			reason = tr("系统密钥库不可用", "system secret store is unavailable")
		}
		fmt.Fprintf(os.Stderr, "⚠ %s。\n", reason)
		fmt.Fprintln(os.Stderr, tr("可改用仅当前用户可读（0600）的明文文件；也可稍后配置 Secret Service 再试。", "You can use a plaintext file readable only by the current user (0600), or configure Secret Service and try again."))
		if strings.ToLower(promptLine(tr("确认用明文文件保存？输入 yes: ", "Save in a plaintext file? Enter yes: "))) != "yes" {
			return "", fmt.Errorf(tr("未保存 key；确认接受明文文件时，可设置 MUXLM_SECRET_BACKEND=file 后重试", "key was not saved; set MUXLM_SECRET_BACKEND=file and retry only if you accept plaintext storage"))
		}
		// The user approved the file backend for this write only. Persisting the
		// choice remains opt-in through MUXLM_SECRET_BACKEND=file.
		backendChoice.explicit = true
	}
	var val string
	intl := region == "intl"
	for {
		val, err = readHiddenPrompt(tr("API key（输入隐藏，回车取消）: ", "API key (hidden input; Enter to cancel): "))
		if err != nil {
			return "", err
		}
		if val == "" {
			return "", fmt.Errorf(tr("已取消", "cancelled"))
		}
		if p.planID() == "custom" {
			proto, base := keyProbeTarget(p, cli, intl)
			fmt.Fprintln(os.Stderr, tr("探测自定义端点…", "Probing custom endpoint…"))
			reachable, code, msg := probe(proto, base, model, val)
			if !reachable || code < 200 || code >= 300 {
				fmt.Fprintln(os.Stderr, msg)
				fmt.Fprintln(os.Stderr, tr("↻ 检测未通过，请重新输入 key", "↻ Probe failed; enter the key again"))
				continue
			}
		} else if note, bad, ambig := checkKey(p, cli, model, intl, val); bad {
			fmt.Fprintln(os.Stderr, note)
			fmt.Fprintln(os.Stderr, tr("↻ key 无效，请重新输入", "↻ Invalid key; try again"))
			continue
		} else if ambig {
			fmt.Fprintln(os.Stderr, note)
			fmt.Fprintln(os.Stderr, tr("⚠ 探测未能确认 key 是否有效。请确认后再保存。", "⚠ The probe could not confirm whether the key is valid. Confirm before saving."))
			confirm := strings.ToLower(promptLine(tr("仍然要保存这个 key 吗？输入 yes 强制保存 [no]: ", "Save this key anyway? Enter yes to force [no]: ")))
			if confirm != "yes" {
				fmt.Fprintln(os.Stderr, tr("已取消，请重新输入。", "Cancelled; enter the key again."))
				continue
			}
		} else if note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
		break
	}
	id := randomID()
	ref := fmt.Sprintf("provider/%s/key/%s", p.providerID(), id)
	backend, err := secretSetWithChoice(p.providerID(), ref, val, backendChoice, runtime.GOOS)
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
		fmt.Fprintln(os.Stderr, tr("✓ 已以 0600 权限存入本地文件 ~/.config/muxlm/providers/<provider>/secrets.json", "✓ Saved to ~/.config/muxlm/providers/<provider>/secrets.json with mode 0600"))
		fmt.Fprintln(os.Stderr, tr("  如需切换到 macOS Keychain / Linux Secret Service，可设 MUXLM_SECRET_BACKEND=keychain（macOS）或 =secret-service（Linux）后重存", "  To switch to macOS Keychain or Linux Secret Service, set MUXLM_SECRET_BACKEND=keychain or =secret-service and save again"))
	} else {
		fmt.Fprintf(os.Stderr, tr("✓ 已保存 key %q [%s]\n", "✓ Saved key %q [%s]\n"), name, backend)
	}
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
		return tr("海外", "international")
	}
	return tr("国内", "domestic")
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

func checkKey(p *Provider, cli, model string, intl bool, key string) (note string, badKey, ambiguous bool) {
	proto, base := keyProbeTarget(p, cli, intl)
	if base == "" {
		return "", false, false
	}
	fmt.Fprintln(os.Stderr, tr("检测 key…", "Checking key…"))
	reachable, code, msg := probe(proto, base, model, key)
	switch {
	case reachable && (code == 401 || code == 403):
		// 服务端明确拒绝：key 无效或无权限。
		return msg, true, false
	case reachable && code >= 200 && code < 300:
		// 探测通过。
		return "", false, false
	case !reachable:
		// 网络/DNS 故障或超时：探测没拿到任何 HTTP 响应。
		// 这种情况下不能证明 key 是否有效，必须由显式确认才能保存。
		return tr("⚠ 暂时连不上端点；无法确认 key 是否有效", "⚠ Endpoint is temporarily unreachable; key validity could not be confirmed"), false, true
	default:
		// 拿到了 HTTP 响应但既不是 2xx 也不是 401/403，例如：
		//   400 — 请求格式错（model id 缺失/错误、参数异常），key 未必错
		//   404 — 端点路径不对
		//   429 — 限流
		//   5xx — 上游故障
		// 这些都不能证明 key 有效。旧逻辑在这里说"鉴权已通过，已保存"是误判；
		// 现在改为显式确认，由调用方提示用户。
		return msg + tr("（探测未返回 2xx/401-403，无法证明 key 有效）", " (probe returned neither 2xx nor 401/403, so key validity is unconfirmed)"), false, true
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
	fmt.Fprintf(os.Stderr, tr("选择端点:\n  1) 国内  %s（默认）\n  2) 海外  %s\n", "Choose endpoint:\n  1) Domestic       %s (default)\n  2) International  %s\n"), p.hostFor(cli, false), p.hostFor(cli, true))
	s := promptLine(tr("请选择 [1]: ", "Choose [1]: "))
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
