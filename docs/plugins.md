# 独立进程插件

核心不依赖 Node.js。插件可以使用 Go、Python、Shell 或其他语言；相应运行时由用户自行安装。旧版 JavaScript 插件不会自动转换，需要用户按本协议适配。插件与机器人拥有相同系统权限，只安装可信代码；私有状态目录并不是安全沙箱。

每个插件目录包含 `manifest.json` 和可执行文件。参见 `examples/plugins/echo`。

```json
{"name":"example","version":"1.0.0","protocol_version":1,"executable":"plugin","args":["--mode","bot"],"commands":["example"],"events":[],"interval_seconds":0,"persistent":false,"timeout_seconds":15}
```

插件名称允许小写字母开头的小写字母、数字、下划线、连字符，最多 64 字符；命令名仅允许大小写字母、数字和下划线。可执行文件必须位于插件目录内，不允许绝对路径、目录逃逸或符号链接；参数通过 argv 原样传递，不经 shell 展开。Windows 使用 `.exe` 文件。解释器程序可使用带 shebang 的脚本（Unix），或用户提供的启动器。

宿主向标准输入发送一行 JSON，插件向标准输出回复一行 JSON。标准输出仅用于协议，日志写标准错误。请求示例：

```json
{"version":1,"type":"command","command":"example","args":["hello"],"text":"hello"}
{"version":1,"type":"event","event":{"type":"message","text":"hello"}}
{"version":1,"type":"tick"}
```

响应：`{"version":1,"text":"reply","error":""}`。每个请求必须得到一个响应，版本必须为 1。单行输出上限 1 MiB，标准错误累计上限 1 MiB。默认单次调用超时 15 秒，清单允许 1–300 秒；取消和超时在 Unix 上终止插件进程组，Windows 上终止直接插件进程。插件应管理自己的子进程；宿主不提供操作系统级子进程沙箱。

`LAOWANGBOT_STATE_DIR` 指向 `<root>/state/<name>`，安装或升级不会替换此目录；`LAOWANGBOT_PROTOCOL_VERSION=1` 标记协议。默认每次调用创建进程。只有声明 `persistent: true` 并且配置事件或间隔时，宿主才可通过 `StartWorker` 启动常驻进程；`Call` 串行调用，`Close` 终止进程。事件筛选和定时调度由机器人集成层负责，插件不会自行获得 Telegram 凭据。

本地安装复制整个目录到 `<root>/plugins/<name>`，已有插件拒绝覆盖。本地安装不会参与远程更新。远程安装与更新仅读取当前项目 `OrionG-hub/laowangbot` 的 `master/plugins/catalog.json`，下载与重定向限定同一项目的 raw.githubusercontent.com 路径；目录中每个文件必须提供 SHA-256。下载和校验成功后通过暂存目录替换，并保留用户状态。远程目录不存在或尚未发布时会明确失败，不能视为已经上线。

目录格式：

```json
{"plugins":[{"manifest":{"name":"example","version":"1.0.0","protocol_version":1,"executable":"plugin","commands":["example"]},"files":[{"path":"plugin","url":"https://raw.githubusercontent.com/OrionG-hub/laowangbot/master/plugins/example/plugin","sha256":"64位十六进制SHA-256"}]}]}
```

开发 API：`Manager{Root: dir}`，`List`、`Load`、`InstallLocal`、`InstallRemote(ctx,name)`、`UpdateRemote(ctx,name)`、`Run(ctx,name,request)`。常驻插件使用 `StartWorker`、`Worker.Call` 和 `Worker.Close`。宿主应拒绝插件命令覆盖内建命令。

响应还可含 `messages`：`{"version":1,"messages":[{"chat_id":"12345","text":"scheduled notification"}]}`，每次最多 20 条。集成层发送这些纯文本通知；命令的 `text` 用于编辑命令消息，事件的 `text` 用于回复事件，定时任务的 `text` 不发送；发送通知请使用 `messages`。使用 `ReplaceLocal(sourceDir)` 可显式替换已有插件；替换后该插件由用户手动维护，不再参与远程更新。

Manifest 完整文件不得超过 64 KiB，且只允许一个 JSON 对象。替换使用隐藏备份；下次列出、加载或安装时自动恢复中断的替换，保留最后有效副本。等待常驻进程的调用也遵守调用方取消和超时。

定时间隔最大为 31,536,000 秒（一年）。响应 `error` 非空时宿主将调用视为失败，不发送同一响应的文本。

CLI 插件操作需停止正在使用同一部署目录的机器人；运行中使用 `.tpm`。消息事件观察新收到的、可寻址的其他账号消息，独立于 sudo/sure 是否消费该消息，不包含历史补发和编辑。

运行实例在启动时固定插件代码及清单副本，安装或更新不改变正在运行的命令和常驻进程依赖文件；重启后加载新版本。状态目录始终保留在原部署中。

## TPM 命令

TPM 仅限账号本人使用；没有自定义源。远程目录固定为本项目的 `plugins/catalog.json`。

| 命令 | 作用 |
|---|---|
| `.tpm` / `.tpm help` | 显示帮助 |
| `.tpm search 关键词` / `.tpm s` | 按名称、说明或命令搜索；不带关键词显示远程列表 |
| `.tpm ls` / `.tpm list` | 已安装插件、版本及远程/手动来源 |
| `.tpm ls -v` / `.tpm lv` | 详细说明、命令、来源、安装/更新时间与修改状态 |
| `.tpm i 名称1 名称2` / `.tpm install 名称` | 安装指定远程插件 |
| `.tpm i all` | 安装目录中尚未安装的插件，保留已有插件 |
| 回复 ZIP 文件发送 `.tpm i` | 导入适配 laowangbot 的手动插件包；已有同名插件拒绝覆盖 |
| `.tpm update` / `.tpm ua` / `.tpm updateAll` | 更新全部已安装的远程插件，跳过手动插件 |
| `.tpm update 名称1 名称2` | 只更新指定远程插件 |
| `.tpm update -f` | 显式覆盖远程插件的本地修改，仍不更新手动插件 |
| `.tpm rm 名称1 名称2` / `.tpm remove all` | 卸载插件代码，保留 `state/<name>` 数据 |
| `.tpm uninstall` / `.tpm un` | `rm` 的别名，必须给名称或 `all` |
| `.tpm upload 名称` / `.tpm ul 名称` | 将插件代码导出为 ZIP，发到当前对话；不包含私有状态及远程更新标记 |
| `.tpm local 目录` / `.tpm replace 目录` | 从服务器目录手动安装/替换 |

安装、更新、卸载都在重启后生效。批量操作逐个处理；失败项不会撤销已经成功的项，最后汇总成功/跳过/失败。默认更新会检测远程插件包中文件的新增、删除及内容修改，检测到修改时跳过；旧安装没有校验基线时也跳过，可检查代码后显式使用 `-f`。从 ZIP 导入的插件一律按手动插件维护。

### ZIP 插件包

ZIP 根目录必须包含 `manifest.json` 和其指定的可执行文件，可包含依赖资源子目录；不能额外包一层目录。不接受原 TeleBox 的 `.ts` 文件直接安装。导入只解包和校验，不运行插件；重启后才加载。压缩包最多 33 MiB，解压内容最多 32 MiB、129 个文件（含清单），ZIP 总条目最多 256 个，并拒绝路径穿越、符号链接和重复路径。插件依赖的 Python、Shell 等运行环境仍需自行提供；Windows 请打包适用的可执行程序。
