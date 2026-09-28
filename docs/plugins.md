# Go 源码插件

插件以 Go 源码分发，安装时编译进 laowangbot。运行不需要 Go；安装、更新和卸载需要与宿主 `go.mod` 匹配的 Go 工具链、Git 及构建依赖的网络访问。依赖版本由宿主固定，插件包不能自带 `go.mod`、`go.work` 或依赖锁文件。

每个包根目录包含 `manifest.json` 和一个 Go 包；资源使用 `go:embed`。示例见 `examples/plugins/echo`：

```json
{"name":"echo","version":"2.0.0","protocol_version":2,"package":".","commands":["plugin_hello"],"timeout_seconds":5}
```

不再支持 `executable`、`args`、`persistent`。根包导出：

```go
func Open(ctx context.Context, host pluginapi.Host, stateDir string) (pluginapi.Plugin, error)
```

`pluginapi.Plugin` 实现 `Handle(context.Context, pluginapi.Request) pluginapi.Response` 和 `Close()`。使用 `github.com/OrionG-hub/laowangbot/pkg/pluginapi`；响应版本使用 `pluginapi.Version`。构造器接收宿主 API 和部署中的私有状态目录；插件应遵守调用上下文取消，并在 `Close` 释放资源。命令、消息事件和 tick 请求由宿主派发，通知使用响应的 `messages`。

插件与宿主同进程、同权限，没有安全沙箱。只安装可信源码；Go 编译及启动检查也会执行包初始化代码。超时不能强行终止不响应上下文的 Go 代码。

## 构建与生效

源码保存在 `<root>/plugins/<name>`，运行时仅使用编译注册表，不执行目录内的程序。状态保存在 `<root>/state/<name>`，更新、卸载及回滚不回退状态。构建区不复制会话、账号数据或插件状态。

首次构建优先使用 `LAOWANGBOT_SOURCE` 指向的本地宿主源码目录；未配置时获取当前宿主版本对应的 GitHub 标签源码。开发版本应配置本地源码。成功构建后保留源码快照，后续插件操作复用该快照；主程序升级则获取目标发布标签的源码。

安装、更新、卸载会暂存源码、生成注册代码、编译宿主并通过 `--check-plugins` 离线检查清单与命令冲突。成功后替换源码和二进制，重启后生效；失败保留旧程序。编译可能耗时，首次可能下载依赖。TPM 和主程序更新共享部署构建锁。

运行中使用 `.tpm`；CLI 管理前停止同一部署实例。Windows 暂不支持源码自动构建替换；即使停止服务，CLI 本身仍占用可执行文件，也不能自行替换。需在外部使用 Go 源码构建，停止服务后手动替换二进制及匹配的源码快照。

## TPM 命令

TPM 仅限账号本人使用；远程源固定为本项目 `master/plugins/catalog.json`。

| 命令 | 作用 |
|---|---|
| `.tpm` / `.tpm help` | 帮助 |
| `.tpm search 关键词` / `.tpm s` | 搜索；不带关键词列出目录 |
| `.tpm ls` / `.tpm list` | 本地插件及来源 |
| `.tpm ls -v` / `.tpm lv` | 版本、命令、更新时间与修改状态 |
| `.tpm i 名称1 名称2` / `.tpm install 名称` | 安装远程源码包 |
| `.tpm i all` | 安装尚未安装的目录项 |
| 回复 ZIP 文件发送 `.tpm i` | 安装手动源码包；拒绝覆盖同名插件 |
| `.tpm update` / `.tpm ua` / `.tpm updateAll` | 更新已安装远程插件；跳过手动插件 |
| `.tpm update 名称1 名称2` | 更新指定远程插件 |
| `.tpm update -f` | 允许覆盖远程插件的本地修改 |
| `.tpm rm 名称1 名称2` / `.tpm remove all` | 卸载源码并重新编译；保留状态 |
| `.tpm uninstall` / `.tpm un` | `rm` 别名，需名称或 `all` |
| `.tpm upload 名称` / `.tpm ul 名称` | 导出源码 ZIP；不含状态和更新标记 |
| `.tpm local 目录` / `.tpm replace 目录` | 手动安装或替换源码包 |

批量操作逐项处理，失败项不撤销此前成功项。默认远程更新检测文件新增、删除和修改；本地有修改或缺少校验基线时跳过，需要检查后显式 `-f`。ZIP 导入和本地替换的包按手动插件维护。

ZIP 根目录直接放清单和 Go 包，不额外包一层目录。不接受符号链接、路径穿越或重复路径。压缩包最多 33 MiB，解压内容最多 32 MiB、129 个文件（含清单），ZIP 条目最多 256 个。

## 主程序升级与回滚

`.update run` 拉取目标标签的宿主源码，携带当前本地插件重新编译，成功后替换并重启；它不会自动更新手动插件源码。Linux/macOS 停止服务后也可运行：

```sh
./laowangbot --source-update v0.1.2 --root /path/to/deployment
./laowangbot --source-rollback --root /path/to/deployment
```

源码回滚同时恢复二进制和对应插件源码快照，保留当前状态。官方安装脚本只下载预编译宿主；发现非空 `plugins/` 或 `.compiled/current.json` 时拒绝覆盖，避免丢失已编译插件。Windows 安装器的 `-Rollback` 同样拒绝源码部署；当前没有自动替换或回滚方案，需停止服务后手动恢复二进制及匹配的源码快照。

## 发布源码目录

```sh
bash scripts/build-plugins.sh v0.1.2
# 审核后显式更新仓库目录（不会推送或发布）：
go run ./cmd/plugin-catalog --tag v0.1.2 --output plugins/catalog.json
```

构建脚本仅生成 `dist/plugins-catalog.json`，不生成平台插件可执行文件。生成器读取示例及 `plugins/` 中的清单，只收录协议 2 的源码包；旧协议打印跳过原因。每个源文件和资源使用实际 SHA-256，下载 URL 固定到指定标签的本仓库 raw 路径。清单位于目录项中，不重复列为下载文件。

发布者需保证标签上的源码与计算哈希时完全一致，并将审核后的目录更新到 `master/plugins/catalog.json`；仅上传 release 附件不会更新机器人使用的目录。本地生成或编译成功不代表远程已发布。

## 旧插件迁移

协议 1 的独立进程包、Shell/Python 启动器、原 TeleBox `.ts` 文件不能直接安装。将逻辑改为上述 Go 包接口，资源嵌入二进制，并把数据写入构造器给定的 `stateDir`。旧目录仅提示迁移，可卸载或用新源码包显式替换；不会自动运行旧可执行文件。

`qdsg` 的本地 OCR Python 脚本已通过 `go:embed` 打包；选择该模式仍需自行配置 Python、ddddocr、OpenCV 和 NumPy，源码编译不会安装这些运行依赖。

## 服务找不到 Go

源码插件安装需要 Go 1.26+ 和 Git。systemd 的 PATH 不会读取 `.bashrc`：终端能运行 Go，不代表服务也能运行。0.1.5 起会额外探测 `/usr/local/go/bin/go` 等标准安装位置，也支持通过**进程环境** `LAOWANGBOT_GO` 指定绝对路径。

已有 Go 的 Linux 服务可配置：

```sh
sudo mkdir -p /etc/systemd/system/laowangbot.service.d
printf '[Service]\nEnvironment="PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"\n' | sudo tee /etc/systemd/system/laowangbot.service.d/20-go-path.conf
sudo systemctl daemon-reload
sudo systemctl restart laowangbot
```

再执行 `.tpm install qdsg`。如果 `/usr/local/go/bin/go version` 本身不存在，需先安装 Go；仅修改 PATH 不会安装工具链。
