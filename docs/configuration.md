# 配置说明

[首页](../README.md) · [安装](../INSTALL.md) · [迁移](migration.md)

## 部署目录

| 路径 | 用途 |
|---|---|
| `config.json` | Telegram API 凭据、GramJS 会话、可选 SOCKS5 代理 |
| `gotd-session.json` | Go 客户端会话状态 |
| `.env` | 可选环境设置 |
| `data/` | 内建命令 JSON 状态 |
| `plugins/` | 已安装 Go 插件源码 |
| `state/<插件名>/` | 插件持久状态，更新代码时保留 |
| `legacy/` | 迁移归档的旧插件源码、资源与数据库 |
| `migration-report.json` | 迁移文件、插件映射、转换日志与警告 |

推荐运行 `--login` 生成账号配置，不要复制示例占位符启动：

```json
{
  "api_id": 123456,
  "api_hash": "YOUR_API_HASH",
  "session": "YOUR_GRAMJS_STRING_SESSION",
  "app_name": "laowangbot"
}
```

`api_id` 必须为正整数，API hash、session 不能为空。可选 `proxy` 对象使用 `socksType: 5`、`ip`、`port` 和可选 `username`、`password`；仅支持 SOCKS5。首次迁移应同时保留有效会话文件，勿混用不同账号的两种会话。

## 环境变量

在部署目录 `.env` 写入 `KEY=value`，修改后重启。文件不执行 shell，不展开变量。

```dotenv
LAOWANGBOT_PREFIX=. 。 $ ，
LAOWANGBOT_SERVICE=laowangbot.service
LAOWANGBOT_UPDATE_REPO=OrionG-hub/laowangbot
```

| 设置 | 含义 |
|---|---|
| `LAOWANGBOT_PREFIX` | 空格分隔的前缀；无有效设置时默认 `. 。 $ ，` |
| `LAOWANGBOT_SERVICE` | 服务重启路径使用的服务名 |
| `LAOWANGBOT_UPDATE_REPO` | 核心程序更新仓库；默认 `OrionG-hub/laowangbot` 的官方 Release |
| `LAOWANGBOT_FISH_ENDPOINT` | Fish 语音端点，仅进程环境；兼容 `MIBOT_FISH_ENDPOINT` |
| `LAOWANGBOT_VERSION` | 构建脚本注入版本，不是运行时版本覆盖 |
| `LAOWANGBOT_ROOT` | Unix 安装脚本默认目标目录；CLI 仍用 `--root` |

核心通过 `Env.Get` 读取的原 `MIBOT_*` 设置兼容同名 `LAOWANGBOT_*`。优先级是：**新名字存在就优先；同一名字下进程环境高于 `.env`**。因此 `.env` 的 `LAOWANGBOT_PREFIX` 优先于进程的 `MIBOT_PREFIX`。显式空的新变量也遮蔽旧变量，具体空值行为由调用方决定。不要同时维护两套名字。

迁移保留已配置前缀，无前缀设置时才采用四个默认字符。MiBox/TeleBox 的 `TB_PREFIX` 由迁移器转换。AI、摘要等插件级设置用 `.help ai`、`.help sum` 查看；含密钥的设置请在收藏夹执行。

下载式安装与核心更新需要对应 Release 的平台构件和校验文件；仓库源码存在不代表某个版本已完成发布。远程插件目录来自当前项目 `master` 分支，不随核心更新仓库覆盖值切换。插件子进程获知 `LAOWANGBOT_STATE_DIR` 和 `LAOWANGBOT_PROTOCOL_VERSION`，参见[插件协议](plugins.md)。

`LAOWANGBOT_PYTHON`：TGS 动画转换使用的 Python 可执行文件，默认 `python3`。仅支持进程环境变量；该环境须安装 `rlottie-python` 和 `Pillow`，并能找到 ffmpeg。见 [TGS 安装说明](../INSTALL.md#yvlu-动态贴纸)。
