package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// StatusData 是 FormatStatus 的全部输入，由 cmd/note 采集：
// 探测结果与状态文件合并后判定三态，再补上文件系统侧的展示值。
type StatusData struct {
	// Running 表示探测到 note 在响应；Unresponsive 表示进程还在但探测失败。
	Running       bool
	Unresponsive  bool
	Pid           int
	URLs          []string
	StartedAt     string
	UptimeSeconds int64
	LogPath       string
	LogExists     bool
	LogSizeBytes  int64
	DBPath        string
	Version       string
	StatePath     string
	StateExists   bool
	// StateOtherDB 非空表示状态文件登记的是同一目录下的另一个数据库，
	// 本次命令不认领它（既不当成「在运行」，也不覆盖）。
	StateOtherDB string
	StalePid     int // 已清理的残留状态文件里的 pid；0 表示没有残留
}

// FormatStatus 把运行情况渲染成与 note info 同风格的对齐键值文本。
func FormatStatus(d StatusData) string {
	title := "note web · 未运行"
	switch {
	case d.Running:
		title = "note web · 运行中"
	case d.Unresponsive:
		title = "note web · 进程存在但不响应"
	}

	rows := [][2]string{}
	if (d.Running || d.Unresponsive) && d.Pid > 0 {
		rows = append(rows, [2]string{"pid", strconv.Itoa(d.Pid)})
	}
	if d.Running && len(d.URLs) > 0 {
		rows = append(rows, [2]string{"地址", strings.Join(d.URLs, "\n")})
	}
	if d.Running && d.StartedAt != "" {
		rows = append(rows, [2]string{"启动时间", d.StartedAt + "（已运行 " + formatUptime(d.UptimeSeconds) + "）"})
	}
	rows = append(rows, [2]string{"日志", fileSummary(d.LogPath, d.LogExists, d.LogSizeBytes)})
	rows = append(rows, [2]string{"数据库", d.DBPath})
	if d.Version != "" {
		rows = append(rows, [2]string{"版本", d.Version})
	}
	rows = append(rows, [2]string{"状态文件", stateSummary(d)})

	labelWidth := 0
	for _, row := range rows {
		if w := displayWidth(row[0]); w > labelWidth {
			labelWidth = w
		}
	}

	// 多值行（如通配监听时的多个地址）按同一列续行；缩进随标签宽度算，
	// 不写死空格数——标签是 CJK 时显示列宽不等于字节数。
	indent := strings.Repeat(" ", labelWidth+4)
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	for _, row := range rows {
		value := strings.ReplaceAll(row[1], "\n", "\n"+indent)
		fmt.Fprintf(&b, "  %s  %s\n", padRight(row[0], labelWidth), value)
	}
	switch {
	case d.Unresponsive:
		b.WriteString("\n进程还在但没有响应：看日志 " + d.LogPath + "，" +
			"必要时结束该 pid 后重新 note web start。\n")
	case !d.Running:
		b.WriteString("\n启动：note web start\n")
	}
	return b.String()
}

// formatUptime 把秒数渲染成中文时长；本地长期运行场景到「小时」即可。
func formatUptime(seconds int64) string {
	switch {
	case seconds < 60:
		return strconv.FormatInt(seconds, 10) + " 秒"
	case seconds < 3600:
		return fmt.Sprintf("%d 分 %d 秒", seconds/60, seconds%60)
	default:
		return fmt.Sprintf("%d 小时 %d 分", seconds/3600, (seconds%3600)/60)
	}
}

// fileSummary 渲染日志/文件类展示值。
func fileSummary(path string, exists bool, size int64) string {
	if !exists {
		return path + "（未创建）"
	}
	return path + "（" + humanSize(size) + "）"
}

func stateSummary(d StatusData) string {
	if d.StateOtherDB != "" {
		return d.StatePath + "（属于另一个数据库 " + d.StateOtherDB + "）"
	}
	if d.StalePid > 0 {
		return d.StatePath + "（残留 pid " + strconv.Itoa(d.StalePid) + "，已清理）"
	}
	if !d.StateExists {
		return d.StatePath + "（不存在）"
	}
	return d.StatePath
}
