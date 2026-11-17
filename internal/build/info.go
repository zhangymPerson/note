// Package build 汇总构建与版本信息：justfile 用 -ldflags -X 注入，未注入时
// （go run、just start、go test）回落到 go build 自带的 vcs 构建信息。
package build

import (
	"runtime"
	"runtime/debug"
	"strings"

	"note/internal/timefmt"
)

var (
	// Version 是源码里的当前版本，构建时可用 -X note/internal/build.Version 覆盖。
	Version = "1.4.0"
	// GitHash / GitDirty / BuildDate 由 justfile 的 build 子命令注入，未注入时为空串。
	// BuildDate 的注入值是构建机本地的带偏移 RFC3339（如 2026-11-03T17:29:33+08:00）：
	// 保留绝对时刻，展示时再统一换算成本机时区的「年月日 时分秒」。
	GitHash   = ""
	GitDirty  = ""
	BuildDate = ""
)

// Info 是一次 Describe() 的结果；Vcs* 保留 go build 的原始值，Git*/BuildDate
// 在注入缺失时已回落成它们，因此调用方无需再判断来源。
// BuildDate 已是展示串（timefmt.Layout，本机时区、无 T/Z 后缀）。
type Info struct {
	Version, GitHash, GitDirty, BuildDate, GoVersion string
	VcsRevision, VcsTime, VcsModified                string
	Injected                                         bool
}

// Describe 读取注入值并回落构建信息，任何构建方式下都返回可用结果。
func Describe() Info {
	info := Info{
		Version:   Version,
		GitHash:   GitHash,
		GitDirty:  GitDirty,
		BuildDate: displayBuildDate(BuildDate),
		GoVersion: runtime.Version(),
		Injected:  GitHash != "" && BuildDate != "",
	}
	if info.Injected {
		return info
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range bi.Settings {
			switch setting.Key {
			case "vcs.revision":
				info.VcsRevision = setting.Value
			case "vcs.time":
				info.VcsTime = setting.Value
			case "vcs.modified":
				info.VcsModified = setting.Value
			}
		}
	}
	if info.GitHash == "" {
		info.GitHash = shortRevision(info.VcsRevision)
	}
	if info.GitDirty == "" && info.VcsModified == "true" {
		info.GitDirty = "true"
	}
	if info.BuildDate == "" {
		// vcs.time 是 UTC；展示口径要与注入值一致，所以换成本地时区的展示串。
		info.BuildDate = displayBuildDate(info.VcsTime)
	}
	return info
}

// displayBuildDate 把注入值 / vcs.time（RFC3339，含 Z 或 ±hh:mm 偏移）统一换成
// 本机时区的「年月日 时分秒」展示串；解析不了就原样返回——时间只是展示项，
// 不能因为一行字符串让 Describe 失败或丢掉已有信息。
func displayBuildDate(value string) string {
	return timefmt.Display(value)
}

// Label 是信息页唯一的版本展示串：注入时带 hash 与构建时间，否则标记开发构建。
func (i Info) Label() string {
	if !i.Injected {
		return i.Version + "（开发构建）"
	}
	parts := make([]string, 0, 3)
	if i.GitHash != "" {
		parts = append(parts, i.GitHash)
	}
	if i.BuildDate != "" {
		parts = append(parts, i.BuildDate)
	}
	if i.GitDirty != "" {
		parts = append(parts, "dirty")
	}
	if len(parts) == 0 {
		return i.Version
	}
	return i.Version + " (" + strings.Join(parts, ", ") + ")"
}

// shortRevision 把 vcs.revision 的完整 hash 收成 7 位，与 git rev-parse --short 对齐。
func shortRevision(revision string) string {
	if len(revision) > 7 {
		return revision[:7]
	}
	return revision
}
