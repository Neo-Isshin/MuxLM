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

Before downloading, the installer checks all of its dependencies and prints the appropriate command for apt, dnf, yum, apk, pacman, zypper, or Homebrew. It can also run that command after asking for confirmation:

```bash
curl -fsSL https://raw.githubusercontent.com/Neo-Isshin/MuxLM/main/install.sh | bash -s -- --install-deps
```

It never runs `sudo` silently. Because the outer one-liner itself requires `curl` and `bash`, install either of those with the system package manager first if it is not already available.

## Linux guide

### Supported systems

MuxLM ships static Linux binaries for AMD64 (`x86_64`) and ARM64 (`aarch64`/`arm64`), with Debian, Ubuntu, and Fedora as the primary targets. Other mainstream distributions such as Arch will usually work as well. Alpine is best-effort because Codex, Claude Code, OpenCode, and their Node.js dependencies may not fully support musl.

The target machine does not need Go or Git. Installation with the command above and self-update require:

- `bash`
- `curl`
- `sha256sum` or `shasum`
- Basic Unix commands including `awk`, `sed`, `mktemp`, and `readlink`

The `coreutils` package provides `sha256sum` on Debian/Ubuntu and Fedora. When something is missing, the installer reports every missing command together and shows the package-manager command that can fix it. `--install-deps` only installs requirements for the MuxLM installer; it does not install Codex, Claude Code, or OpenCode.

### Add the user command directory to PATH

The default installation directory is `~/.local/bin`. If installation succeeds but the shell reports `cld: command not found`, run:

```bash
export PATH="$HOME/.local/bin:$PATH"
cld doctor
```

Bash and Zsh users can add the same `export` line to `~/.bashrc` or `~/.zshrc`. Fish users can run `fish_add_path ~/.local/bin`. Open a new terminal and verify the result with:

```bash
command -v cdx cld opc
```

On Linux, `cld doctor` reports installation dependencies, the selected secret backend, the user command directory, and whether the three AI tools are visible in `PATH`. It only examines local paths and configuration metadata: it does not read API keys, contact a secret service, or make network requests.

### Desktop Linux and headless servers

In a desktop session, MuxLM selects Secret Service when `secret-tool` and a session D-Bus are available. The package that provides `secret-tool` is `libsecret-tools` on Debian/Ubuntu and `libsecret` on Fedora. A Secret Service provider such as an available, unlocked GNOME Keyring or KWallet is also required in the login session.

VPS, NAS, and other headless Linux systems usually do not have a session D-Bus. Select the local file backend explicitly before using MuxLM:

```bash
export MUXLM_SECRET_BACKEND=file
cld doctor
```

Add that `export` line to the login shell configuration if it should persist. The file backend stores keys as plaintext in a private file inside the MuxLM configuration directory and enforces mode `0600`. Do not sync, share, or commit that directory.

The configuration directory is selected in this order:

1. `MUXLM_CONFIG_DIR`
2. `PROVIDERDECK_CONFIG_DIR`
3. `CX_CONFIG_DIR`
4. `$XDG_CONFIG_HOME/muxlm` on Linux
5. `~/.config/muxlm`

Existing `~/.config/muxlm`, ProviderDeck, and ez-switch/cx configurations remain readable through the compatibility rules; no manual migration is required.

### Tool-update boundaries on Linux

```bash
cld update --tool
```

This command finds existing Codex, Claude Code, and OpenCode installations in `PATH`, then invokes their public `update`/`upgrade` commands. Installation-source detection and the upgrade itself belong to the underlying tool. MuxLM does not run `apt`, `dnf`, `pacman`, an AUR helper, or Nix, and it does not switch a tool to a different installation channel.

A tool installed from a distribution package, a read-only system directory, or an administrator-managed deployment may refuse to update itself. MuxLM will still continue with the other tools; update the failed tool with its original package manager or ask the administrator. Likewise, `cld update --self` only updates a MuxLM binary managed by this README's installer. It does not overwrite a system package or manually placed binary.

### Linux troubleshooting

- `cld` is not found: add `~/.local/bin` to `PATH`, then open a new terminal.
- `doctor` cannot find `codex`, `claude`, or `opencode`: install the underlying CLI you plan to use and ensure its command is in `PATH`. Unused tools may be ignored.
- Secret Service cannot save a key: on a desktop, check `secret-tool`, the session D-Bus, and the unlocked keyring; on a headless server, set `MUXLM_SECRET_BACKEND=file`.
- One tool fails during `update --tool`: upgrade it through its original npm, Linuxbrew, system-package, or administrator-managed channel.
- `update --self` refuses to overwrite the binary: the current copy is not managed by the MuxLM installer, so keep using its original installation method.
- If the cause is still unclear, run `cld doctor` and follow its `warning` suggestions. The command does not modify the system.

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
cld k                      # Claude Code + latest pay-as-you-go Kimi
cld k27                    # Claude Code + pay-as-you-go Kimi K2.7 Code
cld kc                     # Claude Code + Kimi Coding Plan
cdx glm                    # Codex + latest GLM
opc ds                     # OpenCode + latest DeepSeek
```

Use `k` for the pay-as-you-go Kimi API and `k27` or `k26` to pin a model. `kc` uses the Coding Plan and its single Model ID, `kimi-for-coding`. The two products use different API keys. The old `kimi`, `kimic`, `kimi26`, and `k3` aliases are retired so that a command which previously selected the Coding Plan cannot silently move to pay-as-you-go billing.

Kimi K2.7 requires Thinking in Claude Code. After `cld k27` opens Claude Code, press `Tab` and confirm that the interface says `Thinking on`. MuxLM configures Kimi's official Anthropic endpoint, the 256K compaction window, and the background-task model automatically.

The shared management commands—such as `list`, `doctor`, `config`, and `update`—work through any of `cdx`, `cld`, or `opc`.

## Quick start

Choose an entry command and a provider alias:

```bash
cld k27                    # Claude Code + pay-as-you-go Kimi K2.7 Code
cld kc                     # Claude Code + Kimi Coding Plan
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
- Configuration overrides are checked in this order: `MUXLM_CONFIG_DIR`, `PROVIDERDECK_CONFIG_DIR`, then `CX_CONFIG_DIR`. With none set, Linux uses `$XDG_CONFIG_HOME/muxlm` or `~/.config/muxlm`, while macOS defaults to `~/.config/muxlm`. Existing ProviderDeck and ez-switch/cx configuration and secrets remain readable without destructive migration.
- Environment precedence is `MUXLM_*`, then `PROVIDERDECK_*`, then `CX_*`.
- The installer keeps compatible `providerdeck` and `ez-switch` command aliases when it can do so safely.

## Build

```bash
go test ./...
go build -ldflags "-X main.appVersion=v2.2.0" -o muxlm .
```

Licensed under the [MIT License](LICENSE). The seed catalog includes community-derived data; see [third-party notices](THIRD_PARTY_NOTICES.md).
