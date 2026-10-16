# rename

自定义品牌名插件：修改 `.status`、`.sysinfo`（以及日志导出头）里显示的 `laowangbot` 字样。

## 命令

| 命令 | 说明 |
| ---- | ---- |
| `.rename <名字>` | 设置自定义品牌名，立即生效 |
| `.rename reset` | 恢复默认 `laowangbot` |
| `.rename` | 查看当前品牌名 |

名字最长 32 个字符，自动去掉换行，存在 `data/brand.json`，重启不丢失。

## 实现

品牌名存在宿主的 `internal/brand` 包里（默认 `laowangbot`），`.status`、`.sysinfo`、日志头里的硬编码字样已改为读取它。插件通过宿主新增的 `get_brand` / `set_brand` 接口读写。
