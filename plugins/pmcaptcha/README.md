# PMCaptcha

内置 Go 版，移植 PMCaptcha `5.1.2 (Merged)`（3173 行版本）。来源 SHA-256：`31e6ba33a2353fe350d6e9d48fa70f2d1f26e406351d6e393af2e03eadc4563b`。

新安装默认关闭（`plugin_enabled=false`），已有配置保留。启用后，陌生私聊默认归档并静音；只有另外开启验证码才发送题目。验证码关闭不等于插件关闭。

```text
.pmc on
.pmc captcha on
.pmc captcha math
.pmc set pass unmute unarchive
.pmc status
```

`.pmc` 和 `.pmcaptcha` 完全同入口。`.pmc h basic/captcha/set/wl/record` 提供分节帮助。原宿主设置面板不适用于 Go；所有配置均可通过以下命令管理。

| 命令 | 行为 |
| --- | --- |
| `on/off/status` | 启停及完整状态；关闭取消待验证，保留配置 |
| `captcha on/off` | 验证码开关；关闭取消待验证 |
| `captcha [mode] math/text/img_digit/img_mixed` | 四种模式；已有题目保留原模式与答案 |
| `set time/tries <N>` | 超时秒数/尝试次数，0 不限；超时按挑战开始时间与最新配置计算 |
| `set keyword <文本>` | 文字关键词；默认“我同意”且无自定义提示时使用 15 种随机问答 |
| `set prompt [文本]` | 自定义纯文本提示，空值清除；支持 `{question}` / `{keyword}` |
| `set fail <动作…>` | block/delete/report/mute/archive/none，或屏蔽/删除/举报/静音/归档/无 |
| `set pass <动作…>` | unmute/unarchive/wl/add_folder/none，或取消静音/取消归档/白名单/加入分组/无 |
| `set folder <ID>` | 0 不启用，1 系统归档，>=2 自定义分组 |
| `folders/fixfolder` | 查看分组规则；清除通过分组 contacts/nonContacts 自动规则 |
| `set initiative on/off` | 我方主动新会话自动通过 |
| `set initiative-failed on/off` | 失败后主动发消息是否允许通过，默认 off；需 initiative on |
| `set history <N>` | 历史消息阈值，<=0 禁用；分页排除当前消息 |
| `set groups <N>` | 共同群阈值，-1 禁用，0 对所有用户满足（兼容 Merged 实际行为） |
| `set wl-words/bl-words <词…>/none` | 包含关键词自动通过/拦截 |
| `set premium allow/ban/only/none` | Premium 策略 |
| `wl / whitelist` | 白名单列表 |
| `add/del/pass <ID/@user>` | 同 `wl add/del/pass`，支持回复消息 |
| `wl del all / wl cleanbots` | 清空白名单/清理机器人 |
| `record [verified/failed]` | 记录摘要/列表，支持 `records` 别名 |
| `record del verified/failed <ID>/all` | 删除记录 |

规则优先级：跳过自身、机器人、777000 → 白名单 → 正在验证 → 已通过 → 历史 → 共同群 → 白词优先于黑词 → Premium → 新验证码。正在验证时我方发消息永不绕过验证；验证码、提示和完成通知的 outgoing 消息也会排除。频道私信仅按宿主确认的 ChannelDM 字段执行通过分组动作。

失败默认归档、静音；`delete` 双方撤回对话。配置了自定义通过分组时，失败从分组显式成员中移除。加入通过分组前清除 contacts/nonContacts 自动规则，保留其他规则与成员。私聊不支持 kick/ban，命令明确拒绝；迁移配置中的旧值保留并在执行时报告无效，不假装成功。

图片由 Go 标准 image/png 和 x/image/basicfont 生成，不安装 Node/canvas。验证码使用 crypto/rand，五位数字或去除易混淆字符的大写字母数字组合；兼容原版，图片回复允许 Levenshtein 编辑距离 <=1（含一个错字、增字、漏字），文本和算术严格匹配（忽略大小写与首尾空白）。图片答案不出现在 caption、文件名、配置或记录中。

状态目录内 `pmcaptcha_config.json` 保存配置，`pmcaptcha_data.json` 保存白名单及通过/失败记录。初次迁移按 `legacy/pmcaptcha_config.json → config.json → pmcaptcha_config.json` 覆盖配置；记录再合并 `legacy/pmcaptcha_data.json → pmcaptcha_data.json`，同 ID 后者优先。`_merged_legacy_v1` 防止已删除记录在重启后复活。持久化成功前不更新内存状态，不宣称保存成功。挑战仅存内存，重启后取消；下一条陌生私聊重新验证。

配置修改刷新已有挑战提示；模式/关键词只影响新题，防止原图与答案不一致。自定义提示按纯文本处理，保留占位符，避免非法 HTML 影响验证码发送。Telegram 动作、清理、发送错误会返回宿主，数据写入失败不会假报成功。

验证采用本地模拟 Host 驱动真实 `Handle` / event / tick 链路，包括四种模式、规则、失败动作、禁用取消、迁移重启与存储失败。当前无第二个 Telegram 账户，**未完成真实陌生人私聊收发、验证码作答及超时/失败动作联调**；模拟测试不代表这些线上链路已验证。
