# qdsg 1.0.0

从用户提供的 qdsg.ts（4164 行，AyuGram Desktop 导出）移植。Go 源码插件（协议 v2，安装时编译进 laowangbot），无 Node；仅本地 OCR 按需启动 Python。原配置 signin_config.json 字段名保留，同机迁移向导会将旧文件自动放入插件状态目录。配置含 API 密钥，文件权限 0600，不应提交 Git。

## 命令

- `qdsg add now @bot text /checkin`：每天当前秒/分/时执行；now 不是立即执行。
- `qdsg add 0 0 8 * * * @bot inline 签到|确认 ai local steps 3 retry 2 interval 5 2000 random 1-50 备注`
- 模式：text、reply、inline、imagechoice、xigua、math（calc）、appcf（app_cf/app-cf）、moon。默认 text；仅指定机器人时默认发送“签到”并随机延迟 0–1 分钟。
- `ai provider "提示词"`；steps N；retry N；interval 分钟；裸数字为等待毫秒；random 最大分钟或 random 最小-最大分钟。末尾未解析文本为备注。
- list/ls 展示全部配置、下次执行时间及上次结果。
- now ID/范围/all 排队执行；结果通过通知和 list 获取。支持 1-3,5、空格分隔、机器人或备注匹配。
- rm、enable、disable 使用相同选择语法。
- edit/set ID/范围/all 属性 值：cron/time、bot、mode、cmd/command、wait、random、remark/note、ai/useai、provider/aiprovider、prompt/aiprompt、steps、retry/retrycount、interval/retryinterval、aitype/aitarget。
- aitype 支持 auto、text、image；random off 关闭延迟；ai on/off 控制识别。
- reorder 重置编号（运行期间拒绝）；reload 重新读取配置及定时计划。
- notify on/off/@user/me；cfbucket ID（off 清除）。
- aiconfig list 遮盖密钥；aiconfig set openai_key/openai_base/openai_model/gemini_key/gemini_base/gemini_model/provider/prompt 值。
- aiconfig addcustom 标识 BaseURL Model Key，aiconfig rmcustom 标识；兼容 OpenAI /v1 与 /api/v3。
- 回复一条消息后 qdsg test provider 提示词测试识别。

## 工作流

| 模式 | 行为 |
| --- | --- |
| text | 发口令，可 AI 识别回复后发答案或选按钮 |
| reply | 可先 /start，匹配回复键盘文本并发送；可接 AI |
| inline | 可先 /start，一次或两级按钮；re:正则(捕获)动态取值；AI 失败从头重跑 |
| imagechoice | 发签到命令，等待新图片与选项，15 秒识别、唯一匹配，接近 30 秒题目时限停止点击 |
| xigua | 点入口、解析目标表情序列，支持右往左，逐次刷新键盘及检查反应 |
| math | 点入口，解析有“计算/算式/请算”或等号锚定的加减乘除，回数字，避免误算日期 |
| appcf | 一步或两步应用入口，请求 Telegram 签名 WebView URL，再交 CF 外援 |
| moon | 识别多轮文字或图片，刷新键盘并监测文本/按钮变化，要求机器人最终确认 |

所有模式以新消息 ID 或同一消息文本/键盘变化判断新回复，不能把旧成功消息当本次结果。原版 Moon/普通文本“流程结束即成功”改为收到明确成功确认；缺少确认报超时。配置 wait=0 时不发默认 /start（reply/inline/appcf）；继承旧 sendStart 字段。

## OCR 与 CF

OCR 完整保留原 Python 的多字符三种清洗、HSV/距离变换、单数字通道、候选评分和 Q1NJ 修正；修复原脚本 CC_STAT_AREA 未加 cv2. 导致清洗 v3 退回原图的问题。脚本通过 go:embed 编入程序。选择 ai local 时才运行，不自动安装依赖。

准备插件专用环境（在插件状态目录执行）：

```sh
python3 -m venv .venv
.venv/bin/python -m pip install ddddocr opencv-python-headless numpy
```

也可通过 QDSG_PYTHON 指定已有解释器。缺依赖时明确报错；测试不下载 OCR 模型或安装依赖。

CF 保留 cfbox/pending-<job>.json、solved-<job>.json 本机信箱和 https://api.npoint.io/<bucket> 云端竞速。外部执行端需另行运行并匹配旧版信箱协议；本插件不自带浏览器解题器。带 secondaryCommand 的应用流程要求 proof，并通过 webview_data 回传；普通链接/单步应用以执行端完成后机器人的新确认判断成功。作业带 ID，云端覆盖连续两次确认，过期预算、失败/超时和收单清理均保留。外援声称完成不等于签到成功。

## 生命周期与验证边界

单工作线程、最多 64 个运行/排队任务；同 ID 不重入。每轮最多 4 分钟；重试在队列中延期，随机延迟不阻塞其他任务。删除/禁用会跳过尚未开始的任务，正在进行的一轮可继续结束；停止插件取消当前请求。计划由 Go cron 秒级解析，使用运行环境本地时区。插件需宿主 0.1.1 host bridge；manifest 声明最小方法权限。

解析边界：Go RE2 正则不支持 JS 的 lookaround/backreference；不匹配时给出找不到按钮错误。任务 steps 1–30、retry 0–100、单次 wait 0–60000ms，拒绝超界值。外部 OCR 准确性、机器人协议变化、真实 Telegram/WebView 与 CF 外援均需部署后实测。

单元测试覆盖解析、CRUD、配置迁移、密钥遮盖、消息新鲜度、队列去重/重试与取消；假宿主集成覆盖八种模式，HTTP 模拟覆盖 OpenAI/Gemini/custom；本机 CF 信箱测试覆盖 proof 回传及清理。测试不访问真实 Telegram、不发送外部消息、不调用真实 AI/CF。
