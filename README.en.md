# laowangbot

[简体中文](README.md) · [English](README.en.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md)

**Go core · Compiled-in Go source plugins · Migratable Telegram UserBot**

laowangbot keeps the existing built-in commands without requiring Node.js in the core. Plugins ship as Go source and compile into the host during installation; media features use ffmpeg when needed.

## Status

The project is published at `github.com/OrionG-hub/laowangbot`. Official Releases provide platform binaries and checksums; no Docker image is published, so build the container locally from source.

## Quick start

### Fresh installation (login required)

Installing and running the official binary requires neither Go nor Node.js. Source plugin management and source upgrades require Go and Git. The installer downloads the latest official Release, verifies SHA-256, prompts for login and starts a background task. The Release must contain the platform binary and `checksums.txt`; use the source workflow below until these assets are published.

```sh
# Linux
sudo bash -c 'bash <(curl -fsSL https://raw.githubusercontent.com/OrionG-hub/laowangbot/master/scripts/install.sh) --root /opt/laowangbot'

# macOS
bash <(curl -fsSL https://raw.githubusercontent.com/OrionG-hub/laowangbot/master/scripts/install.sh) --root "$HOME/laowangbot-data"
```

```powershell
# Windows PowerShell
& ([scriptblock]::Create((Invoke-RestMethod 'https://raw.githubusercontent.com/OrionG-hub/laowangbot/master/scripts/install.ps1'))) -Root "$env:LOCALAPPDATA\laowangbot"
```

Linux uses systemd, macOS a per-user launchd job, and Windows a user logon scheduled task. Send `.ping` and `.help` in Saved Messages after installation to verify connectivity. Login requires your Telegram API ID, API hash, phone number and verification code. See [INSTALL.md](INSTALL.md) for all options.

### One-command migration from an existing bot

Already running mibot-lite, MiBox or TeleBox? Use this migration command instead of the fresh installer above. Select the old bot and directory; configuration, session and supported data are transferred automatically. A valid existing session requires no new API ID entry or login.

```sh
# Linux
sudo bash -c 'bash <(curl -fsSL https://raw.githubusercontent.com/OrionG-hub/laowangbot/master/scripts/install.sh) --wizard --root /opt/laowangbot'

# macOS
bash <(curl -fsSL https://raw.githubusercontent.com/OrionG-hub/laowangbot/master/scripts/install.sh) --wizard --root "$HOME/laowangbot-data"
```

```powershell
# Windows PowerShell
& ([scriptblock]::Create((Invoke-RestMethod 'https://raw.githubusercontent.com/OrionG-hub/laowangbot/master/scripts/install.ps1'))) -Wizard -Root "$env:LOCALAPPDATA\laowangbot"
```

Use an empty destination. Linux migration stops and disables the old systemd service, restoring its original state on failure. For other launchers, stop the old instance and disable automatic startup first. The source directory is preserved. If you are at `API ID:`, press Ctrl+C and use the commands above. Inspect a nonempty destination instead of deleting it. Unknown plugins are archived and still require code adaptation. See the [migration guide](docs/migration.md).

### Source installation and migration

Source builds require Go 1.26. From the source directory, build and install a trusted local binary:

```sh
bash scripts/build.sh ./laowangbot

# Linux
sudo bash scripts/install.sh --binary "$PWD/laowangbot" --root /opt/laowangbot

# macOS
bash scripts/install.sh --binary "$PWD/laowangbot" --root "$HOME/laowangbot-data"
```

```powershell
# Windows PowerShell
go build -trimpath -o .\laowangbot.exe .\cmd\laowangbot
.\scripts\install.ps1 -Binary "$PWD\laowangbot.exe" -Root "$env:LOCALAPPDATA\laowangbot"
```

```sh
# Migration
bash scripts/install.sh --binary "$PWD/laowangbot" \
  --migrate /path/to/old-bot --from auto \
  --root "$HOME/laowangbot-data"
```

Stop the old bot before migration and use an empty destination. On Linux, prefix migration with `sudo` or add `--no-service`. Choose either a fresh install or migration.

## Commands

Default prefixes are `.`, `。`, `$`, and `，`; migration preserves custom prefixes. Use `.help command` for details. Runtime messages are mostly Chinese; four README translations do not imply complete runtime localization.

| Category | Commands |
|---|---|
| Operations | `ping` `status` `memory` `sysinfo` `version` / `ver` `help` / `h` `update` `restart` `log` `bf` |
| Configuration / plugins | `prefix` `alias` `privacy` `tpm` |
| Utilities | `calc` `rate` `tr` `gt` `whois` `ip` `bin` `ids` `dc` `speedtest` / `st` |
| AI | `ai` `sum` |
| Messages / media | `yvlu` `eatgif` `eat` `eat2` `sticker` `t` `ts` `tk` `re` `save` `dme` `da` |
| Moderation | `ban` `unban` `kick` `mute` `unmute` `sb` `unsb` `refresh` `aban` |
| Account / delegation | `acn` / `autochangename` `sudo` `sure` |

## Documentation

Detailed references below are currently in Simplified Chinese.

- [Deployment](INSTALL.md): Linux / macOS / Windows / Docker
- [Configuration](docs/configuration.md): `LAOWANGBOT_*`, legacy `MIBOT_*`
- [Migration](docs/migration.md): mibot-lite / MiBox / TeleBox
- [Plugin protocol](docs/plugins.md): `.tpm`, local / remote
- [Architecture and development](docs/architecture.md): Go, JSON, processes

Migration imports known configuration and archives old assets/plugins. Unknown TypeScript plugins require manual adaptation; they are not automatically executable. Do not run old and new instances with the same session at once.

## Validation and provenance

```sh
go test ./... -coverprofile=coverage.out
go test -tags=integration ./...
go tool cover -func=coverage.out
go vet ./...
```

Run unit tests, integration tests and coverage checks as shown below. CI definitions and cross-compilation do not prove target-platform runtime success. Live Linux/Windows/Docker deployment and real Telegram account checks still need validation; no upstream memory figures are claimed as current measurements.

Derived from [MiCat-S/mibot-lite](https://github.com/MiCat-S/mibot-lite), commit `dbc404a2323061c4abd7f13088622e1d045153fa`, on local branch `refactor/laowangbot`. Original attribution and [LGPL-2.1](LICENSE) are retained. The Noto Sans SC subset uses [SIL OFL 1.1](internal/statuscard/NotoSansSC-OFL.txt).

## Plugin manager (TPM)

Installing, updating or removing plugins rebuilds the host and requires Go and Git. Windows does not support automatic source rebuilding and replacement; build externally, stop the service, then manually replace the binary and its matching source snapshot.

Use `.tpm s keyword` to search, `.tpm ls -v` for details, `.tpm i name1 name2` or `.tpm i all` to install, `.tpm update` to update remote plugins, `.tpm rm name` to uninstall, and `.tpm ul name` to export ZIP. Reply to an adapted ZIP package with `.tpm i` to install manually. The source stays fixed to this project. Manual plugins are never remotely updated, local changes are protected by default, and changes take effect after restart. See the [TPM guide](docs/plugins.md#tpm-命令).
