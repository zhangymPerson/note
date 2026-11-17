// Package timefmt 是面向用户的**时间展示**口径：年月日 + 时分秒、24 小时制、
// 按系统时区换算、不带 T/Z 后缀（如 2026-10-27 09:02:03）。
//
// 它只负责展示。库里存的仍是 UTC 秒级文本（internal/store 的 timeLayoutUTC，
// 可按键排序），两者分工明确：存储用 UTC 保证排序与迁移稳定，展示用本机时区
// 保证用户看到的是自己的墙钟时间。
package timefmt

import "time"

// Layout 是所有面向用户的时间展示格式：年月日 + 时分秒，无 T/Z 后缀。
const Layout = "2006-01-02 15:04:05"

// 需要识别并换算的输入格式：库里/状态文件里的 UTC 秒级文本、带偏移的 RFC3339
// （justfile 注入的构建时间就是这种）。
var parseLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05Z",
}

// DisplayTime 把任意时刻按系统时区渲染成 Layout。
func DisplayTime(t time.Time) string {
	return t.In(time.Local).Format(Layout)
}

// Display 把已知的时间串按系统时区渲染成 Layout：
//   - UTC 秒级文本 / RFC3339（含 Z 或 ±hh:mm）→ 本机时区的年月日 时分秒；
//   - 已经是 Layout 的串 → 原样返回（可重复调用）；
//   - 解析不了 → 原样返回，绝不把用户的时间变成一个空串或 panic。
func Display(value string) string {
	if value == "" {
		return value
	}
	for _, layout := range parseLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return DisplayTime(t)
		}
	}
	return value
}
