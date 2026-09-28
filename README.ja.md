# laowangbot

[简体中文](README.md) · [English](README.en.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md)

**Go コア · 独立プロセスのプラグイン · 移行対応 Telegram UserBot**

laowangbot は既存の組み込みコマンドを維持し、別プロセスで動く拡張機能を追加します。コアに Node.js は不要です。プラグインの言語ランタイムは個別に用意し、一部のメディア機能は ffmpeg を使用します。

## 現在の状態

プロジェクトは `github.com/OrionG-hub/laowangbot` で公開されています。公式 Release に各プラットフォームのバイナリとチェックサムがあります。Docker イメージは未公開のため、コンテナはソースからローカルでビルドしてください。

## クイックスタート

### ワンコマンド導入

Go や Node.js は不要です。インストーラーが最新の公式 Release を取得して SHA-256 を検証し、対話式ログイン後にバックグラウンドタスクを開始します。対応バイナリと `checksums.txt` の公開が必要です。未公開時は下のソースからの手順を使ってください。

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

Linux は systemd、macOS はユーザーの launchd、Windows はユーザーログオン時のタスクを使用します。導入後は「保存したメッセージ」で `.ping` と `.help` を送り、接続を確認してください。ログインには Telegram API ID、API hash、電話番号と確認コードが必要です。全オプションは [INSTALL.md](INSTALL.md) を参照してください。

### ソースからの導入と移行

ソースのビルドには Go 1.26 が必要です。ソースディレクトリでビルドし、信頼するローカルバイナリを各環境に導入します。

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

移行前に旧インスタンスを停止し、空の移行先を指定してください。Linux の移行にも `sudo` を付けるか、`--no-service` を指定します。新規導入と移行はどちらかを選んでください。

## コマンド

既定の接頭辞は `.`、`。`、`$`、`，` です。移行時はカスタム設定を保持します。詳細は `.help コマンド` で確認できます。実行時メッセージは主に中国語です。README の翻訳は実行時の完全な多言語対応を意味しません。

| 分類 | コマンド |
|---|---|
| 運用 | `ping` `status` `memory` `sysinfo` `version` / `ver` `help` / `h` `update` `restart` `log` `bf` |
| 設定・プラグイン | `prefix` `alias` `privacy` `tpm` |
| 検索・ツール | `calc` `rate` `tr` `gt` `whois` `ip` `bin` `ids` `dc` `speedtest` / `st` |
| AI | `ai` `sum` |
| メッセージ・メディア | `yvlu` `eatgif` `eat` `eat2` `sticker` `t` `ts` `tk` `re` `save` `dme` `da` |
| グループ管理 | `ban` `unban` `kick` `mute` `unmute` `sb` `unsb` `refresh` `aban` |
| アカウント・権限 | `acn` / `autochangename` `sudo` `sure` |

## ドキュメント

以下の詳細ドキュメントは現在、簡体字中国語で提供しています。

- [導入と運用](INSTALL.md): Linux / macOS / Windows / Docker
- [設定](docs/configuration.md): `LAOWANGBOT_*`, legacy `MIBOT_*`
- [移行](docs/migration.md): mibot-lite / MiBox / TeleBox
- [プラグイン仕様](docs/plugins.md): `.tpm`, local / remote
- [設計と開発](docs/architecture.md): Go, JSON, processes

移行は既知の設定を変換し、旧リソースとプラグインを保存します。未対応の TypeScript プラグインには手動の適応が必要で、そのまま実行できません。同じセッションを旧・新インスタンスで同時に使用しないでください。

## 検証と由来

```sh
go test ./... -coverprofile=coverage.out
go test -tags=integration ./...
go tool cover -func=coverage.out
go vet ./...
```

以下の単体・統合テストとカバレッジ確認を実行します。CI 定義やクロスコンパイルだけでは対象環境の動作を保証できません。Linux、Windows、Docker の実配備と Telegram 実アカウントの検証は未完了です。上流のメモリ使用量を本プロジェクトの測定値として扱いません。

[MiCat-S/mibot-lite](https://github.com/MiCat-S/mibot-lite) の `dbc404a2323061c4abd7f13088622e1d045153fa` を起点とし、ローカルブランチは `refactor/laowangbot` です。元の著作者表示と [LGPL-2.1](LICENSE) を維持します。Noto Sans SC のサブセットは [SIL OFL 1.1](internal/statuscard/NotoSansSC-OFL.txt) で配布されます。
