# trace

自动回应插件，移植自 [TeleBox-Plugins/trace](https://github.com/TeleBoxOrg/TeleBox-Plugins/tree/main/trace)。

给指定用户发的所有消息、或包含指定关键字的消息，自动贴 emoji 回应。

## 命令

| 命令 | 说明 |
| ---- | ---- |
| 回复某条消息 + `.trace 👍👎` | 追踪该用户，以后他发的每条消息自动贴这些表情 |
| 回复某条消息 + `.trace` | 取消追踪该用户 |
| `.trace kw add <词> 👍` | 含该关键字的消息自动贴表情 |
| `.trace kw del <词>` | 删除关键字追踪 |
| `.trace status` | 查看追踪统计 |
| `.trace clean` | 清除所有追踪 |
| `.trace reset` | 重置全部数据 |
| `.trace log true\|false` | 操作回执是否保留（默认 true，false 则 10 秒后删除回执） |
| `.trace big true\|false` | 大号表情动画（默认 true） |

## 说明

- 数据存在插件 state 目录的 `trace.json`，重启不丢失。
- 只监听别人发的消息，自己发的会忽略。
- 仅支持标准 emoji；原版的会员自定义表情需要解析消息 entity，laowangbot 插件接口目前拿不到，暂不支持。
- 需要宿主提供 `react` 能力（`messages.SendReaction` 的透传），已随本插件一起加入宿主（纯新增）。
