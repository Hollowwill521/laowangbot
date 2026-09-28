# 验证记录

本地工具链为 Go 1.27.1（项目最低版本 Go 1.26）。在 macOS arm64 工作站运行：

- `go test -count=1 -race ./...`：通过。
- `go test -count=1 -coverprofile=/tmp/laowangbot-coverage-final.out ./...`：通过；总体语句覆盖率 38.3%。覆盖率是全仓库结果，不能单独代表 Telegram 线上行为。
- `go test -count=1 -tags=integration ./tests/integration`：通过；真实构建的二进制完成 mibot-lite、MiBox、TeleBox fixture 迁移，离线检查、备份、恢复和插件安装/替换。
- `go vet ./...`：通过。
- `bash tests/deployment/install_test.sh`：通过；迁移目标保护、更新备份、失败保留和 launchd 回滚模拟通过。
- `bash -n scripts/build.sh scripts/release.sh scripts/install.sh scripts/install-service.sh`：通过。
- `bash scripts/release.sh v0.1.0-dev`：生成 Linux amd64/arm64、macOS amd64/arm64、Windows amd64/arm64 六个构件及 SHA-256 清单。

Windows PowerShell 脚本和 Dockerfile 已进入 CI；本机没有 PowerShell、Docker，因此未在本机执行。CI 矩阵覆盖目标平台的测试入口，但目标系统服务、Docker 容器启动、真实 Telegram 登录、代理、ffmpeg、外部 AI/语音服务和 `--verify` 均未在本次本地验证。远程下载式安装依赖 GitHub Release 的六平台构件和 SHA-256 清单；发布状态以 GitHub 为准。

`.bf` 备份继续只收录账号配置、`.env` 和顶层 `data/*.json`；插件代码在 `plugins/`，插件私有状态在 `state/<name>/`，需要随部署目录单独备份。迁移会将旧插件归档到 `legacy/`，不会把这些目录伪装成已兼容状态。

## 发布前复核

补充并通过回归测试：启动锁检查先于会话写入；备份和恢复拒绝 `data` 符号链接；运行实例固定插件代码及清单，更新后仅在重启时生效，插件状态继续写入原部署。Windows 测试不将 POSIX 权限位当作 ACL 验证。README 提供 `master` 分支的远程单条安装命令。
