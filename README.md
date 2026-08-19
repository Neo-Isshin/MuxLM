# MuxLM

> Switch between providers and models in Codex, Claude Code, and OpenCode — without editing config files or leaving API keys scattered everywhere.

One line to try: `cdx glm52` — launches Codex with GLM 5.2.

[简体中文](README.zh-CN.md) · [Catalog reference](CATALOG.md)

MuxLM is a **lightweight switcher, not a proxy**: the underlying CLI connects directly to your chosen provider, and the temporary launch configuration stays isolated from your existing global config — it leaves nothing behind.

## What it is

| Command | Launches | Example |
| --- | --- | --- |
| `cdx` | Codex | `cdx glm` → Codex with the latest GLM |
| `cld` | Claude Code | `cld k3` → Claude Code with Kimi K3 |
| `opc` | OpenCode | `opc ds` → OpenCode with the latest DeepSeek |

All three are the **same binary**; only the default CLI differs. `cld` is also the management entry point (`list`, `config`, `update`, …), so you only need to remember three words: `cdx`, `cld`, `opc`.

> Compatibility comes from the catalog; MuxLM does **no protocol translation**. For the full list of any provider/model, run `<entry> list` or browse the [Catalog reference](CATALOG.md).

## Why MuxLM

- One binary, three entry points: `cdx`, `cld`, `opc`
- The catalog (provider/model list) updates independently, and **works offline** via cache and an embedded fallback
- Supports official provider routes, relay routes, and custom providers
- API keys are stored in a local 0600 file by default; opt in to macOS Keychain or Linux Secret Service with `MUXLM_SECRET_BACKEND=keychain` / `=secret-service`
- Named keys, domestic/international routes
- No daemon, database, GUI, or protocol proxy

## Install

Prebuilt releases support macOS and Linux on ARM64 and AMD64. You must also install the underlying CLI you want to launch (Codex / Claude Code / OpenCode) and make it available in `PATH`.

```bash
curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash
```

The installer verifies the release checksum, installs `muxlm` to `~/.local/bin`, and creates the `cdx`, `cld`, and `opc` commands. If it says that directory is not in `PATH`, add it with the command it prints.

Before downloading, it checks all dependencies and prints the right command for apt, dnf, yum, apk, pacman, zypper, or Homebrew. It can also run that command after asking for confirmation:

```bash
curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash -s -- --install-deps
```

It never runs `sudo` silently. The outer one-liner itself needs `curl` and `bash`; install either with the system package manager first if it is not already available.

## 30-second start

No setup required — just launch with a model short name:

1. For example, `opc ds` — starts OpenCode with the latest DeepSeek.
2. The **first time** you use a provider, MuxLM asks for its API key (input is hidden), validates it, and stores it securely.
3. Later launches of any model under that provider reuse the saved key automatically — no need to re-enter.

> A provider keeps a single key, shared by every model in that provider.

## Common models

**Two rules:**

- A model short name alone → its publisher's **official** route (e.g. `cld k3`).
- To use another source, prefix the model name with a **source alias** (e.g. `opc or k3` for OpenRouter).

| Command | Meaning |
| --- | --- |
| `cld def` | Claude Code's own subscription and default model |
| `cdx def` | Codex's own account and default model |
| `opc def` | OpenCode's own config and default model |
| `cld k3` | Claude Code + Kimi **official** K3 |
| `opc or k3` | OpenCode + Kimi K3 via **OpenRouter** |
| `cld sf k27` | Claude Code + Kimi K2.7 Code via SiliconFlow |
| `cld k27` | Claude Code + pay-as-you-go Kimi K2.7 Code |
| `cld kc` | Claude Code + Kimi Coding Plan |
| `cld glm` | Claude Code + latest GLM (`cdx glm52` pins 5.2) |
| `cld qc` | Claude Code + Bailian Coding Plan |
| `cdx q` | Codex + latest Qwen |
| `opc or` | OpenCode + OpenRouter |
| `cdx m --intl` | Codex + latest MiniMax M3 on the **international** route |
| `opc ds` | OpenCode + latest DeepSeek |

**About `def`:** It uses no MuxLM provider and reads no stored key. Instead it clears routing overrides and lets the CLI fall back to its native account, configuration, and default model (Claude Code returns to subscription models like Opus and Fable; Codex / OpenCode return to their own logins and config). Pass native CLI arguments after `--`, e.g. `cld def -- --model opus`.

**Same name, no collision:** `cld k3` is always Kimi official, while `<entry> or k3` selects OpenRouter. If the chosen source does not currently offer that model, MuxLM says so — it **never** silently swaps in that source's default. Existing pinned aliases such as `sfv4f` and `ork3` keep working.

**Three useful options:**

```bash
opc ds -m deepseek-v4-pro   # Override the model ID
cld glm -- "fix the bug"    # Pass everything after -- to the underlying CLI
cdx glm --dry-run           # Preview configuration without launching
```

## Essential commands

Any of `cdx`, `cld`, `opc` runs the management commands below (`<entry>` stands for whichever you use):

| Command | What it does |
| --- | --- |
| `<entry> list` | List providers, aliases, and models |
| `<entry> config` | View and manage providers and keys |
| `<entry> add` | Add a provider key or custom provider |
| `<entry> set-key <alias>` | Add another named key |
| `<entry> remove <alias>` | Remove local provider configuration |
| `<entry> update` | Update the model list (see [Updates](#updates)) |
| `<entry> doctor` | Run local, read-only diagnostics |
| `<entry> audit-probes` | Probe every catalog endpoint with a fake key to verify protocol paths |
| `<entry> version` | Show app and catalog versions |
| `<entry> --help` | Show full help |

### Interface language

MuxLM automatically uses Chinese for a Chinese system locale and English for an English locale. Other system languages fall back to English. Run `cld config` and choose **Language / 语言** to select Auto, English, or 中文; the choice is saved in `settings.json`. For scripts, `MUXLM_LANG=en|zh|auto` temporarily overrides the saved choice.

## Updates

Four forms, each with one clear purpose:

| Command | Updates |
| --- | --- |
| `cld update` | The model list |
| `cld update --tool` | The installed Codex / Claude Code / OpenCode |
| `cld update --self` | MuxLM itself |
| `cld update --all` | All of the above, in order |

> `cdx update …` and `opc update …` do the same thing.

**Updating the three AI tools (`--tool`):** MuxLM finds them in `PATH`, recognizes whether each came from npm, Homebrew, or an official installer, and hands it to its official updater — it never changes your install method. Missing tools are skipped; one failure does not stop the others. If a tool is too old for a safe automatic update, MuxLM asks you to upgrade it once via the original method instead of accidentally opening its interactive interface.

**Automatic catalog updates:** Every normal startup also checks the model list; a valid update is written atomically and can take effect immediately. If the check fails, MuxLM keeps the last valid cache or the embedded catalog — so **it works offline**. A newer app version is only reported; the binary is **never** silently replaced.

```bash
MUXLM_AUTO_UPDATE=0 cld glm      # Disable startup checks
MUXLM_UPDATE_DEBUG=1 cld glm     # Show update diagnostics
cld update                        # Update the model list now
```

MuxLM installed via the command in this README can update itself. If the current copy came from elsewhere, MuxLM stops and asks you to use the original installation method instead of overwriting an unknown file.

## Privacy

- The child process receives only the **selected provider's** key; other provider keys are removed from its environment.
- Codex and OpenCode get **disposable configuration directories** — your global config is untouched.
- API keys live in a local 0600 file by default; opt back into system keychains via `MUXLM_SECRET_BACKEND=keychain` / `=secret-service`.

## Advanced

### Host your own catalog

Serve `catalog.json` from a static HTTPS URL (preferably with `ETag` or `Last-Modified`), then set:

```bash
export MUXLM_CATALOG_URL=https://example.com/catalog.json
```

Until you move it, the default catalog is served from this GitHub repository. Downloads are limited to 2 MiB and checked with strict schema validation, monotonic immutable revisions, rollback protection, tombstones, and trust-field pinning.

### Catalog update safety

Catalog updates are not limited to additions: a revision may add providers/models, retire and remove old models or aliases, and move `latest`. To prevent surprises from later updates, these guarantees always hold:

- Revisions are **monotonic and immutable** — a given revision cannot be modified or rolled back.
- Retired models leave a **permanent tombstone**, so a retired version alias can never be reused by a later update.
- **Official model short names** and "source + model" targets cannot be silently redirected elsewhere by a later update.
- A provider's **trust fields** cannot be changed silently.

### Migrating from older tools

The config directory is chosen in this order: `MUXLM_CONFIG_DIR` → `PROVIDERDECK_CONFIG_DIR` → `CX_CONFIG_DIR`. With none set, Linux uses `$XDG_CONFIG_HOME/muxlm` or `~/.config/muxlm`, and macOS defaults to `~/.config/muxlm`. Environment precedence is `MUXLM_*` > `PROVIDERDECK_*` > `CX_*`. Existing ProviderDeck and ez-switch (cx) configuration and secrets remain readable with **no destructive migration**; the installer keeps `providerdeck` and `ez-switch` compatible aliases when it is safe to do so.

### Build from source

```bash
go test ./...
go build -ldflags "-X main.appVersion=v2.5.0" -o muxlm .
```

Licensed under the [MIT License](LICENSE). The seed catalog includes community-derived data; see [third-party notices](THIRD_PARTY_NOTICES.md).
