# 架构与开发

[首页](../README.md) · [配置](configuration.md) · [插件协议](plugins.md)

## 运行结构

```text
Telegram → gotd 客户端 → 命令注册/权限/前缀/别名 → Go 内建命令
                          └→ 扩展宿主 → 宿主接口 → 已编译 Go 插件
部署目录 ← 账号会话 / JSON 状态 / 插件状态
监督进程 → 启动服务子进程 → 重启时重新加载二进制
```

Go 核心保留上游内建功能；没有必需的 JavaScript 引擎或 Node.js 服务。插件在安装或更新时编译进主程序，运行时由宿主按声明权限提供接口；OCR 和媒体操作按需调用 Python/ffmpeg。插件不构成权限沙箱。

| 模块 | 职责 |
|---|---|
| `cmd/laowangbot`、`internal/cli` | 入口、登录、检查、迁移、插件管理 |
| `internal/app`、`internal/bot` | Telegram 连接、事件分发、重试与输出处理 |
| `internal/command`、`internal/commands` | 命令解析、内建命令与配置转换 |
| `internal/plugin`、`internal/extensions` | 插件安装、校验、协议、事件与定时集成 |
| `internal/migration`、`internal/mibox` | 迁移事务、归档、已知 JSON/SQLite 转换 |
| `internal/platform` | 监督进程、跨平台锁与进程操作 |
| `internal/backup`、`internal/store` | 备份恢复与状态存储 |
| `scripts`、`deploy` | 构建、打包与平台部署 |

内建命令随主程序更新；插件代码与 `state/` 分开存放。远程插件仅从当前项目目录读取，逐文件验证 SHA-256；手工本地安装或替换的插件由用户维护。未知旧 TypeScript 插件只归档并标记待适配。

## 开发

需要 Go 1.26。新增内建命令时实现 `Register(a *app.App)`，调用 `a.Registry.Register`，并在 `internal/commands/register.go` 注册。授权借用需要单独审视 `internal/commands/sudo` 白名单，不能默认授权新命令。独立扩展优先参考[插件协议与示例](plugins.md)。

```sh
go build ./...
go vet ./...
bash scripts/build.sh
```

测试文件和测试数据仅保留于本地，不随仓库发布。单元测试、集成测试与覆盖率检查需要本地测试副本；仓库 CI 仅执行构建、静态检查和安装脚本语法检查。历史覆盖率不能作为当前代码的验证结果。

`bash scripts/release.sh vX.Y.Z` 生成六个平台/架构构件和校验清单；Go 交叉编译不能证明目标平台运行正确。真实 Linux systemd、Windows 计划任务、Docker 及 Telegram 登录验收仍需相应环境。`--verify` 使用真实账号与外部服务，先停用同会话的其他实例。

不承诺固定 RSS 或性能倍数；应分别测量核心、常驻插件及临时媒体进程。当前多语言范围是 README，内建命令大部分提示仍为中文。
