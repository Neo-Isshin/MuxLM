# ProviderDeck

Launch Codex, Claude Code, or OpenCode with the provider and model you want—using one short command.

[简体中文](README.zh-CN.md)

ProviderDeck is a lightweight CLI switcher, not a proxy. The selected CLI connects directly to the provider, while your existing global CLI configuration stays untouched.

## Highlights

- One binary, three entry points: `cdx`, `cld`, and `opc`
- Independently updated provider/model catalog with an embedded offline fallback
- Bare aliases follow `latest`; version aliases stay pinned while published
- Catalog updates can add models, retire old models and aliases, and move `latest`
- API keys use macOS Keychain or Linux Secret Service when available
- Disposable Codex/OpenCode configuration; no global config pollution
- Named keys, domestic/international routes, and custom providers
- No daemon, database, GUI, or protocol proxy

## Install

Prebuilt releases support macOS and Linux on ARM64 and AMD64.

```bash
curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/ProviderDeck/main/install.sh | bash
```

The installer verifies `SHA256SUMS`, installs `providerdeck` to `~/.local/bin`, and creates the `cdx`, `cld`, and `opc` links. It will not overwrite unrelated commands unless `FORCE=1` is set. You also need the underlying CLI you plan to launch in `PATH`.

## Quick start

```bash
cld glm                           # Claude Code + latest GLM
cdx m --intl                      # Codex + MiniMax international route
opc ds                            # OpenCode + latest DeepSeek
opc ds -m deepseek-v4-pro         # Override the model ID
cld glm -- "fix the bug"          # Pass arguments through
cdx glm --dry-run                 # Preview without launching
cdx doctor                        # Local, read-only diagnostics
```

When a key is missing, ProviderDeck prompts with hidden input, validates it once, and stores it for later use.

| Entry | Launches | Route |
|---|---|---|
| `cdx <alias>` | Codex | OpenAI-compatible |
| `cld <alias>` | Claude Code | Anthropic-compatible |
| `opc <alias>` | OpenCode | Either supported route |

Provider compatibility comes from the catalog; ProviderDeck does not translate protocols.

## Commands

```text
<entry> list                 List providers, aliases, and models
<entry> config               View/manage providers and keys
<entry> add                  Add a provider key or custom provider
<entry> set-key <alias>      Add another named key
<entry> remove <alias>       Remove local provider configuration
<entry> update               Refresh catalog and check app version
<entry> doctor               Run local, read-only diagnostics
<entry> version              Show app and catalog versions
<entry> --help               Show full help
```

## Catalog updates

Every normal startup checks the catalog. App-version checks use a separate one-hour interval to stay within GitHub API limits; when both checks are due they run in parallel with a shared 2.5-second deadline. A valid catalog is stored atomically and can be used by the same command. On failure, ProviderDeck keeps the last valid cache or embedded catalog.

Catalog revisions are immutable and monotonic (`YYYY-MM-DD.N`). An update may add models/providers, remove retired models and their version aliases, or move `latest`. A version alias that remains published cannot be rebound. Existing provider identities, endpoints, key identities, CLI capabilities, and wire protocols cannot be silently changed by a catalog-only update; those changes require a reviewed binary release.

When removing a tagged model, keep its alias and historical target in the top-level `retired_tags` map. Tombstones are permanent: they prevent an old alias from being reused after a fresh install or configuration reset.

ProviderDeck only reports a newer app version—it never silently replaces the binary.

```bash
PROVIDERDECK_AUTO_UPDATE=0 cld glm       # Disable startup checks
PROVIDERDECK_UPDATE_INTERVAL=1h cld glm  # Throttle catalog checks
PROVIDERDECK_RELEASE_INTERVAL=0 cld glm  # Check app version every launch
PROVIDERDECK_UPDATE_DEBUG=1 cld glm      # Show diagnostics
cld update                               # Force a check now
```

### Host your own catalog

Serve `catalog.json` from a static HTTPS URL, preferably with `ETag` or `Last-Modified`:

```bash
export PROVIDERDECK_CATALOG_URL=https://example.com/catalog.json
```

Downloads are limited to 2 MiB and checked with a strict schema, rollback protection, immutable revisions, and trust-field pinning. Release metadata and the installer can be moved independently with `PROVIDERDECK_RELEASE_API_URL` and `PROVIDERDECK_INSTALL_URL`.

## Keys, privacy, and migration

- Only the selected key is passed to the child CLI; other provider keys are scrubbed.
- Codex uses a temporary `CODEX_HOME`; OpenCode uses a temporary `OPENCODE_CONFIG_DIR`.
- Fresh installs store configuration under `~/.config/providerdeck` with private permissions and atomic writes. Existing installs keep safely using `~/.config/cx`; if both directories exist, ProviderDeck reads the new one first and falls back per file to the old one.
- Existing `CX_*` variables and `ez-switch` Keychain/Secret Service records remain readable for a non-destructive migration. `PROVIDERDECK_*` takes precedence when both are set.
- Without a system secret store, keys fall back to a mode-`0600` local file with a visible warning.

## Build

```bash
go test ./...
go build -ldflags "-X main.appVersion=v2.0.0" -o providerdeck .
```

Licensed under the [MIT License](LICENSE). The seed catalog includes community-derived data; see [third-party notices](THIRD_PARTY_NOTICES.md).
