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

向导先询问旧人形和旧部署目录。Linux 随后按实际 WorkingDirectory 自动匹配 systemd 服务：一个匹配自动选中，多个匹配列数字菜单，未匹配才提示服务名（提供对应旧人形默认值）。服务列表中的别名会去重，停止前再次核验目录。Linux 的 `--migrate` 和 `--source-stopped` 同样检查匹配的 systemd 服务；手动停止不代表已禁用自启动。多个匹配服务中，未选中的服务必须已停止且禁用。旧 Docker/PM2/后台实例及 macOS 启动器请先自行停止并禁用自动启动，再使用 `--source-stopped`。脚本不会猜测或终止无关进程。

脚本自动下载并校验 laowangbot，迁移配置、会话、已支持数据和插件清单，检查新部署并安装后台任务。旧目录保留；无法直接运行的旧插件归档在 `legacy/`，需自行适配代码。迁移前记录旧 systemd 服务的运行及自启动状态，停止并禁用其开机启动后再迁移。失败时恢复原自启动状态，只重新启动原先运行的旧服务；无法确认新服务已经停止时，不恢复旧服务，避免双实例运行。成功后旧服务保持停止且禁用，服务器重启也不会随开机启动。由 timer/socket 等触发的服务必须先停用触发器，脚本检测到触发关系时会拒绝迁移。

本机已有二进制时可加 `--binary /path/to/laowangbot`（Windows 为 `-Binary`），无人值守沿用 `--migrate 旧目录 --from 类型`。远程单条迁移命令见 README，或在原安装命令末尾加 `--wizard` / `-Wizard`。

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

旧外部源码插件的 CLI 维护入口（先停止机器人；TPM 源码写入暂时禁用）：

```sh
./laowangbot --plugin install-local --plugin-source /path/to/adapted-plugin --root /path/to/new-laowangbot
./laowangbot --plugin replace-local --plugin-source /path/to/adapted-plugin --root /path/to/new-laowangbot
./laowangbot --plugin list --root /path/to/new-laowangbot
```

官方程序已内置 `monitor`、`qdsg` 和 `bh`，迁移配置后直接使用相应命令。旧 `assets/baohao/baohao_config.json` 自动复制到 `state/bh/baohao_config.json`，保留原任务和通知配置；也可通过 `.bh tpm i` 回复配置文件导入。TPM 源码安装、替换、更新及卸载暂时禁用，`.tpm list` 可查看内置及已有外部插件。

PMCaptcha 的 `assets/pmcaptcha/pmcaptcha_config.json`、`pmcaptcha_data.json` 和面板旧 `config.json` 迁至 `state/pmcaptcha`；根目录 `pmcaptcha_userdata` 的旧配置、用户数据迁至 `state/pmcaptcha/legacy`。插件首次加载合并这些来源，使用标记避免已删除的旧记录在重启后重新出现。新安装默认关闭；迁移已有启停配置按原值保留。

## 兼容依据与回退

上游起点：MiCat-S/mibot-lite `dbc404a2323061c4abd7f13088622e1d045153fa`。TeleBox 数据结构核对基线：`cf53d468b5bd4c47254dc98e2cc55418e2cb84f8`。这些是结构核对依据，不代表所有版本、全部插件或真实账号联调通过。

回退时停止新实例，再启动保留的旧部署。新实例运行后产生的数据不会自动反向同步；必要时先备份。不要让旧、新实例同时使用同一 Telegram 会话。

向导固定选项统一输入数字；多个服务匹配时支持空格或逗号多选（`1 2 3`、`1,2,3`）或 `all` 全选，回车/`0` 取消。迁移前逐项停止并禁用所选服务，失败时按各自原运行、自启动状态恢复；未选服务仍须已停止且禁用。未匹配时服务名输入可回车使用建议值。标题/输入提示为青色，选项/成功为绿色，注意事项为黄色，错误为红色。非终端或设置 `NO_COLOR` 时不输出颜色。目录和服务名仍需输入文本。
