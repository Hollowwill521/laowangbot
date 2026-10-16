# music_bot

多音源音乐搜索插件，移植自 [TeleBox-Plugins/music_bot](https://github.com/TeleBoxOrg/TeleBox-Plugins/tree/main/music_bot)。

通过与第三方 Telegram 音乐机器人对话来搜索音乐，拿到音频文件后转发回你发命令的聊天。

## 命令

| 命令 | 说明 |
| ---- | ---- |
| `.mbs 关键词` | 综合搜索（关键词中包含来源会自动识别，如 `.mbs 洛天依 网易云`） |
| `.mbkw 关键词` | 酷我源 |
| `.mbkg 关键词` | 酷狗源 |
| `.mbqq 关键词` | QQ 音乐源 |
| `.mbne 关键词` | 网易云音乐源 |
| `.mbvk 关键词` | VK 音乐源（@vkmusic_bot） |
| `.mbym 关键词` | YouTube Music 源（@ttaudiobot） |
| `.music_bot <来源> 关键词` | 手动指定来源：search / kugou / kuwo / qq / netease / vk / ym |

## 依赖的第三方机器人

- `@music_v1bot`（默认源）
- `@vkmusic_bot`
- `@ttaudiobot`

首次使用某个机器人时，插件会自动给它发 `/start` 初始化一次。如果机器人一直没响应，请手动打开它点一下 Start 再试。

## 与原版的差异

- 原版是把机器人发来的音频**下载后重新发送**（可带标题）；laowangbot 的 `send_file` 有 512KB 限制，音乐文件通常更大，所以这里改为**直接转发**机器人那条音频消息，无大小限制，但不带自定义标题。
- 原版的解禁 / 静音机器人操作在 laowangbot 的 Host API 里没有对应能力，已略去。
