# laowangbot

[简体中文](README.md) · [English](README.en.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md)

**Go 核心 · 編譯內建原始碼外掛 · 可遷移的 Telegram UserBot**

laowangbot 保留既有內建指令；核心不需要 Node.js。外掛以 Go 原始碼分發，安裝時編譯進主程式；部分媒體功能按需使用 ffmpeg。

## 目前狀態

專案發布於 `github.com/OrionG-hub/laowangbot`。官方 Release 提供各平台執行檔與校驗檔；尚未發布 Docker 映像，容器請從原始碼本機建置。

## 快速開始

### 一鍵部署

首次安裝及執行官方二進位檔不需 Go 或 Node.js；管理原始碼外掛及原始碼升級需要 Go 和 Git。安裝器下載最新官方 Release、校驗 SHA-256，互動登入後啟動背景工作。Release 必須包含對應平台執行檔與 `checksums.txt`；尚未發布時請使用下方原始碼方式。

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

Linux 使用 systemd，macOS 使用目前使用者的 launchd 工作，Windows 使用使用者登入排程工作。安裝後在「已儲存訊息」傳送 `.ping`、`.help` 驗證連線；登入需要 Telegram API ID、API hash、手機號碼與驗證碼。完整參數請參閱 [安裝指南](INSTALL.md)。

### 原始碼安裝與遷移

原始碼建置需要 Go 1.26。在原始碼目錄建置，再依平台安裝可信本機執行檔：

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

遷移前停止舊實例，目標目錄必須為空；Linux 遷移同樣需要 `sudo`，或加入 `--no-service`。全新安裝與遷移範例擇一執行。

## 指令概覽

預設前綴為 `.`、`。`、`$`、`，`，遷移保留自訂前綴。使用 `.help 指令` 查詢詳情。執行時訊息主要為中文，四種 README 語言不代表完整執行時國際化。

| 類別 | 指令 |
|---|---|
| 執行維護 | `ping` `status` `memory` `sysinfo` `version` / `ver` `help` / `h` `update` `restart` `log` `bf` |
| 設定與外掛 | `prefix` `alias` `privacy` `tpm` |
| 查詢工具 | `calc` `rate` `tr` `gt` `whois` `ip` `bin` `ids` `dc` `speedtest` / `st` |
| AI | `ai` `sum` |
| 訊息與媒體 | `yvlu` `eatgif` `eat` `eat2` `sticker` `t` `ts` `tk` `re` `save` `dme` `da` |
| 群組管理 | `ban` `unban` `kick` `mute` `unmute` `sb` `unsb` `refresh` `aban` |
| 帳號與授權 | `acn` / `autochangename` `sudo` `sure` |

## 文件導覽

以下詳細文件目前以簡體中文提供。

- [安裝與部署](INSTALL.md): Linux / macOS / Windows / Docker
- [設定](docs/configuration.md): `LAOWANGBOT_*`, legacy `MIBOT_*`
- [遷移](docs/migration.md): mibot-lite / MiBox / TeleBox
- [外掛協定](docs/plugins.md): `.tpm`, local / remote
- [架構與開發](docs/architecture.md): Go, JSON, processes

遷移匯入已知設定並封存舊資源及外掛；未知 TypeScript 外掛需手動適配，不能直接執行。不要讓新舊實例同時使用相同會話。

## 驗證與來源

```sh
go test ./... -coverprofile=coverage.out
go test -tags=integration ./...
go tool cover -func=coverage.out
go vet ./...
```

執行以下單元測試、整合測試及覆蓋率檢查。CI 設定與交叉編譯不代表目標平台已成功執行；Linux、Windows、Docker 真實部署與 Telegram 真實帳號仍待驗收，不將上游記憶體數字當成本專案實測。

源自 [MiCat-S/mibot-lite](https://github.com/MiCat-S/mibot-lite) 的 `dbc404a2323061c4abd7f13088622e1d045153fa`，本機分支為 `refactor/laowangbot`。保留原作者歸屬與 [LGPL-2.1](LICENSE)。Noto Sans SC 字型子集依 [SIL OFL 1.1](internal/statuscard/NotoSansSC-OFL.txt) 發布。

## 外掛管理（TPM）

外掛安裝、更新及移除會重新編譯主程式，需要 Go 和 Git。Windows 尚不支援自動建置替換，需外部建置後停止服務，手動替換二進位檔及相符的原始碼快照。

`.tpm s 關鍵詞` 搜尋，`.tpm ls -v` 查看詳情，`.tpm i 名稱1 名稱2` 或 `.tpm i all` 安裝，`.tpm update` 批次更新，`.tpm rm 名稱` 解除安裝，`.tpm ul 名稱` 匯出 ZIP。回覆已適配的 ZIP 檔案並傳送 `.tpm i` 可手動安裝。遠端來源固定為本專案；手動外掛不自動更新，本機修改預設受保護，變更於重新啟動後生效。詳見[完整用法](docs/plugins.md#tpm-命令)。

同機遷移不需複製設定：Linux 執行 `sudo bash scripts/install.sh --wizard --root /opt/laowangbot`，依選單選擇舊人形和部署目錄。macOS 移除 sudo 並使用使用者目錄；Windows 使用 `-Wizard`。詳見[遷移精靈](docs/migration.md#同机一键迁移向导)。
