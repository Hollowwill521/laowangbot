package react

// helpText 是 .react 的帮助面板，只讲怎么用：开关、范围、关键词写法、群名单、status。
// 哪些对话点不了、哪些消息会跳过这些行为边界不再往面板里堆——面板已经够长，而且这些
// 都不是用户能改的设定。判定规则看 react.go / guard.go 的注释（跳过目前不打日志）。
func helpText(prefix string) string {
	p := prefix + "react"
	return "<b>关键词点赞（" + p + "）</b>\n\n" +
		"命中关键词的那条消息，用本账号给它点一个表情。只点赞，不转发也不通知；" +
		"词表和群表都跟 monitor 各管各的。\n\n" +
		"<b>开关与范围</b>\n" +
		"• <code>" + p + " on</code> / <code>" + p + " off</code>　总开关，默认关着\n" +
		"• <code>" + p + " all on</code>　所有超级群和频道都点\n" +
		"• <code>" + p + " all off</code>　只点下面列出的群（默认）\n" +
		"• <code>" + p + " big on</code>　用大表情动画\n\n" +
		"<b>关键词和表情</b>（写法与 monitor 相同）\n" +
		"• <code>" + p + " keyword add 我要</code>　命中就点默认表情\n" +
		"• <code>" + p + " keyword add 我要 ❤️</code>　这个词单独用 ❤️，最后一个片段是表情\n" +
		"• <code>" + p + " keyword emoji 序号 ❤️</code>　改已有关键词的表情（也可以写原词）\n" +
		"• <code>" + p + " keyword add re:^\\d{12}$ 🎂</code>　正则同样能带表情，正则也支持 <code>re:/词/i</code>\n" +
		"• <code>" + p + " keyword del 序号</code>（也可以写原文）/ <code>" + p + " keyword clear</code>\n" +
		"• <code>" + p + " emoji 👍🏻</code>　默认表情：词上没单独写表情时用它\n" +
		"只收普通 Unicode 表情；Premium 自定义表情要先解析，这一版没做。\n\n" +
		"<b>群</b>（群名单只决定在哪些群里点，不决定用哪个表情）\n" +
		"• <code>" + p + " group add</code>　把当前这个群加进来\n" +
		"• <code>" + p + " group add -1001234567890</code>　也可以是 @公开用户名 或 t.me 链接\n" +
		"• <code>" + p + " group del 序号</code>（也可以写群 ID）/ <code>" + p + " group clear</code>\n\n" +
		"• <code>" + p + " status</code>　看当前配置和成功、丢弃的计数"
}
