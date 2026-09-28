# laowangbot 重构实施计划

目标：将 mibot-lite 重构为独立维护的 laowangbot，支持迁移、跨平台部署和可安装插件。

## 已确定的范围

- 显示名称、二进制及服务名称：laowangbot。
- Go module：github.com/OrionG-hub/laowangbot。
- 目标仓库：https://github.com/OrionG-hub/laowangbot；尚未创建或发布。
- 兼容现有 config.json、数据目录及 MiBox、mibot-lite、TeleBox 迁移。
- 默认前缀增加中文逗号 `，`，保留现有命令和用户自定义前缀。
- 支持 Linux、macOS、Windows 的 amd64/arm64，以及 Docker。
- 提供一键部署、迁移报告、四语 README、架构及开发部署文档。
- 保留上游许可证及来源说明。

## 实施顺序

- [x] 建立测试基线，记录当前失败和环境限制。
- [x] 集中项目身份与发布配置，拆分启动入口、迁移和平台服务职责。
- [x] 增加新环境变量命名，兼容旧配置，测试默认及自定义前缀。
- [x] 实现统一迁移入口：检查源、暂存转换、冲突保护、迁移报告；不修改来源目录。
- [x] 核对 TeleBox 会话、插件配置、数据库与安装清单格式，逐项添加迁移测试。
- [x] 实现插件安装及更新接口：有对应实现的远程插件切换为本项目版本；手动插件需用户适配。
- [x] 实现跨平台锁、服务管理、发布产物及更新策略。
- [x] 实现 Shell、PowerShell 一键部署及 Docker 部署。
- [x] 重写四语 README，并提供配置、迁移及插件开发示例。
- [x] 运行单元测试、迁移及安装集成测试、覆盖率和跨平台构建；明确未实测平台。

## 已批准的接口

用户已批准 Go 核心 + 独立进程 JSON Lines 插件协议。核心不强制依赖 Node.js；手动插件由用户适配。

## 已核实的技术事实

- 原项目命令编译进 Go 二进制，没有动态插件加载机制。
- TeleBox 使用 teleproto StringSession，config.json 包含 api_id、api_hash、session。
- TeleBox 的 plugins/*.ts 依赖 Plugin 接口，支持命令、事件监听、定时任务及生命周期。
- TeleBox TPM 安装记录位于 assets/tpm/plugins.json，包含来源 URL、更新时间和可选内容哈希。
- 已将应用锁、登录锁和进程 CPU 统计拆分为平台实现。
- 重启支持跨平台监督进程；Windows 停止后由 PowerShell 安装器更新/回滚。

验证策略：本地临时目录构造迁移来源和插件样例，通过本地 HTTP 服务测试下载校验与回滚；真实 Telegram 验证需要可用账号；跨平台构建不等同于目标系统运行验证。
