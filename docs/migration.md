# 迁移到 laowangbot

[首页](../README.md) · [安装](../INSTALL.md) · [配置](configuration.md) · [插件适配](plugins.md)

支持从 mibot-lite、MiBox、TeleBox 部署导入账号、会话和已知配置。迁移不是任意旧插件的自动转译；保留文件与功能可运行是两件事。

## 同机一键迁移向导

在源码目录运行以下命令，按提示选择旧人形和旧目录，无需手动复制任何配置：

```sh
# Linux（systemd，新目标目录应为空）
sudo bash scripts/install.sh --wizard --root /opt/laowangbot

# macOS（当前用户 launchd）
bash scripts/install.sh --wizard --root "$HOME/laowangbot-data"
```

```powershell
# Windows（当前用户登录计划任务）
.\scripts\install.ps1 -Wizard -Root "$env:LOCALAPPDATA\laowangbot"
```

向导依次询问：①旧人形 `mibot-lite` / `MiBox` / `TeleBox`；②包含 `config.json` 的旧部署目录；③旧实例停止方式。Linux 选择 `1`（回车默认）后输入 systemd 服务名，脚本验证该服务的工作目录与旧目录一致，再停止服务。已自行停止或其他平台选择 `2`，停止旧 Docker/PM2/后台实例后选择 `1` 确认继续（回车默认取消）。脚本不会搜索、终止无关进程。

脚本自动下载并校验 laowangbot，迁移配置、会话、已支持数据和插件清单，检查新部署并安装后台任务。旧目录保留；无法直接运行的旧插件归档在 `legacy/`，需自行适配代码。失败时尝试恢复由脚本停止的旧 systemd 服务；无法确认新服务已经停止时，不启动旧服务，避免双实例运行。成功后旧 systemd 服务保持停止，其开机自启动配置不会被修改，请避免重启服务器后自动启动旧实例。

本机已有二进制时可加 `--binary /path/to/laowangbot`（Windows 为 `-Binary`），无人值守沿用 `--migrate 旧目录 --from 类型`。向导目前是本地待发布修改；发布新脚本后，远程单条命令在原安装命令末尾加 `--wizard` / `-Wizard` 即可。

## 手动指定参数（可用于自动化）

1. 停止旧机器人，备份部署目录；SQLite 如使用 WAL，先正常关闭并完成 checkpoint，避免最新数据仍只在 WAL 文件中。
2. 使用空的、独立的新目标目录；不能覆盖现有部署，也不要放在源目录内部。
3. 运行迁移并阅读 JSON 报告。
4. 离线检查后启动，逐项验证常用命令和配置。保留旧部署用于回退。

```sh
./laowangbot --migrate /path/to/old-bot --from auto --root /path/to/new-laowangbot
./laowangbot --check --root /path/to/new-laowangbot
./laowangbot --supervise --root /path/to/new-laowangbot
```

`--from` 可明确选 `mibot-lite`、`mibox`、`telebox`。自动识别根据 `assets/` 和 `package.json` 等标记判断；目录缺少标记时请显式指定。迁移不改源文件，先写暂存目录再提交目标；已有目标非空会拒绝。

## 数据与转换范围

复制 `config.json`、可选 `gotd-session.json`、`.env`、`data/`；`assets/`、旧 `plugins/` 归档在 `legacy/`，不会作为新协议插件直接加载。自定义前缀保留，`TB_PREFIX` 转换为兼容设置；更新仓库和服务配置切换到 laowangbot。

已知转换依据 `internal/commands/import.go`，包含：

| 旧配置 | 处理范围 |
|---|---|
| AI、save/prometheus、speedtest | 按各命令转换器映射配置 |
| sum、da、dme、aban、autochangename、yvlu、t、sticker、privacy | 已知 JSON 路径复制到对应命令状态 |
| alias | SQLite `aliases(original, final)` 转换 |
| sudo、sure | 支持的授权 SQLite 数据转换 |
| whois | 已知 JSON 历史/缓存；不转换 v2 `records.sqlite` |

同一目标有多个旧版来源时优先已定义的新版本路径；已存在的目标数据不覆盖。转换失败、未识别数据库或未映射资源不等于成功恢复功能，需检查报告并人工核对。旧 `--import-mibox` 保留为局部数据导入入口；完整部署迁移推荐 `--migrate`。

## 插件来源与报告

`migration-report.json` 记录文件、来源类型、插件状态、转换日志和警告。识别为现有内建命令的旧插件使用 Go 内建实现，更新随当前项目主程序；未知插件标记 `needs-adaptation`。旧远程来源只作为历史信息保留，不继续从旧仓库更新。

手工适配后安装独立进程插件（CLI 操作前停止机器人；运行中使用 `.tpm`）：

```sh
./laowangbot --plugin install-local --plugin-source /path/to/adapted-plugin --root /path/to/new-laowangbot
./laowangbot --plugin replace-local --plugin-source /path/to/adapted-plugin --root /path/to/new-laowangbot
./laowangbot --plugin list --root /path/to/new-laowangbot
```

Telegram 对应 `.tpm local 路径`、`.tpm replace 路径`、`.tpm list`。安装/替换后重启加载；显式本地替换将插件改为手工维护。远程 `.tpm install 名称` / `.tpm update 名称` 仅读取 `OrionG-hub/laowangbot` 的 `master` 分支目录。仓库提供 `echo` 示例（命令 `plugin_hello`），该 Shell 示例仅适用于 Unix，可用 `.tpm install echo` 安装。

## 兼容依据与回退

上游起点：MiCat-S/mibot-lite `dbc404a2323061c4abd7f13088622e1d045153fa`。TeleBox 数据结构核对基线：`cf53d468b5bd4c47254dc98e2cc55418e2cb84f8`。这些是结构核对依据，不代表所有版本、全部插件或真实账号联调通过。

回退时停止新实例，再启动保留的旧部署。新实例运行后产生的数据不会自动反向同步；必要时先备份。不要让旧、新实例同时使用同一 Telegram 会话。

向导固定选项统一输入数字；systemd 停止方式回车默认选 1。标题/输入提示为青色，选项/成功为绿色，注意事项为黄色，错误为红色。非终端或设置 `NO_COLOR` 时不输出颜色。目录和服务名仍需输入文本。
