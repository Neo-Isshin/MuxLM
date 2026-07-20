# MuxLM

Launch Codex, Claude Code, or OpenCode with the provider and model you want—using one short command.

[简体中文](README.zh-CN.md)

MuxLM is a lightweight CLI switcher, not a proxy. The selected CLI connects directly to the provider, and MuxLM keeps its temporary launch configuration separate from your existing global configuration.

## Why MuxLM

- One binary, three entry points: `cdx`, `cld`, and `opc`
- A provider/model catalog that updates independently and still works offline
- Catalog updates can add providers and models, remove retired models, and move `latest`
- API keys use macOS Keychain or Linux Secret Service when available
- Named keys, domestic/international routes, and custom providers
- No daemon, database, GUI, or protocol proxy

## Install

Prebuilt releases support macOS and Linux on ARM64 and AMD64. You must also install the underlying CLI you want to launch and make it available in `PATH`.

```bash
curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash
```

The installer verifies the release checksum, installs `muxlm` to `~/.local/bin`, and creates the `cdx`, `cld`, and `opc` commands. Add that directory to `PATH` if the installer asks you to.

## First-time setup

First, check that MuxLM can find the AI CLI you plan to use:

```bash
cld doctor
```

You only need the tools you actually use. A `not found` warning for an unused `codex`, `claude`, or `opencode` installation does not affect the other entry points. Next, refresh the model list and see the available aliases:

```bash
cld update
cld list
```

Then choose a provider and launch. On first use, MuxLM asks for the provider's API key without showing it on screen, validates it, and stores it securely:

```bash
cld k3                     # Claude Code + Kimi K3
cdx glm                    # Codex + latest GLM
opc ds                     # OpenCode + latest DeepSeek
```

The shared management commands—such as `list`, `doctor`, `config`, and `update`—work through any of `cdx`, `cld`, or `opc`.

## Quick start

Choose an entry command and a provider alias:

```bash
cld glm                    # Claude Code + latest GLM
cdx m --intl               # Codex + MiniMax international route
opc ds                     # OpenCode + latest DeepSeek
```

The most useful options are:

```bash
opc ds -m deepseek-v4-pro  # Override the model ID
cld glm -- "fix the bug"   # Pass everything after -- to the underlying CLI
cdx glm --dry-run          # Preview configuration without launching
```

`cdx` launches Codex, `cld` launches Claude Code, and `opc` launches OpenCode. Provider compatibility comes from the catalog; MuxLM does not translate protocols.

## Essential commands

```text
<entry> list                 List providers, aliases, and models
<entry> config               View and manage providers and keys
<entry> add                  Add a provider key or custom provider
<entry> set-key <alias>      Add another named key
<entry> remove <alias>       Remove local provider configuration
<entry> update               Update the model list
<entry> update --tool        Update detected Codex, Claude Code, and OpenCode CLIs
<entry> update --self        Update MuxLM
<entry> update --all         Update everything
<entry> doctor               Run local, read-only diagnostics
<entry> version              Show app and catalog versions
<entry> --help               Show full help
```

## Catalog updates

MuxLM checks the catalog on every normal startup. A valid update is stored atomically and can take effect immediately. If the check fails, MuxLM keeps using the last valid cache or the embedded catalog.

Updates are not limited to additions: a catalog revision may add providers/models, retire and remove old models or aliases, and move `latest`. Permanent tombstones prevent retired version aliases from being reused. Strict validation also blocks rollback, modified revisions, and silent changes to provider trust fields.

MuxLM only reports a newer app version during startup; it never silently replaces the binary.

```bash
MUXLM_AUTO_UPDATE=0 cld glm       # Disable startup checks
MUXLM_UPDATE_DEBUG=1 cld glm      # Show update diagnostics
cld update                        # Update the model list now
```

## Update all three AI tools in one command

You do not need to look up three separate upgrade commands. One command updates every detected Codex, Claude Code, and OpenCode installation:

```bash
cld update --tool
```

`cdx update --tool` and `opc update --tool` do the same thing.

MuxLM finds the three tools in `PATH` and hands each one to its official updater. The tool recognizes whether it came from npm, Homebrew, or an official installer and stays on that installation channel. Missing tools are skipped, and one failed update does not stop the others.

If an installed version is too old to support a safe automatic update, MuxLM asks you to upgrade it once through its original installation method instead of accidentally opening its interactive interface.

## Update the model list and MuxLM

Each update form has one clear purpose:

```bash
cld update           # Update only the model list
cld update --tool    # Update the three installed AI tools
cld update --self    # Update only MuxLM
cld update --all     # Run all of the above in order
```

MuxLM installations created by the command in this README can update themselves. If the current copy came from somewhere else, MuxLM stops and asks you to use the original installation method instead of overwriting an unknown file.

## Host your own catalog

Serve `catalog.json` from a static HTTPS URL, preferably with `ETag` or `Last-Modified`, then set:

```bash
export MUXLM_CATALOG_URL=https://example.com/catalog.json
```

Until you move it, the default catalog is served from this GitHub repository. Downloads are limited to 2 MiB and checked with a strict schema, monotonic immutable revisions, rollback protection, tombstones, and trust-field pinning.

## Privacy and migration

- Only the selected key is passed to the child CLI; other provider keys are removed from its environment.
- Codex and OpenCode receive disposable configuration directories.
- New configuration uses `~/.config/muxlm`. Existing ProviderDeck and ez-switch/cx configuration and secrets remain readable without destructive migration.
- Environment precedence is `MUXLM_*`, then `PROVIDERDECK_*`, then `CX_*`.
- The installer keeps compatible `providerdeck` and `ez-switch` command aliases when it can do so safely.

## Build

```bash
go test ./...
go build -ldflags "-X main.appVersion=v2.1.0" -o muxlm .
```

Licensed under the [MIT License](LICENSE). The seed catalog includes community-derived data; see [third-party notices](THIRD_PARTY_NOTICES.md).
