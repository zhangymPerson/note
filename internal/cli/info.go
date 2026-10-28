package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rivo/uniseg"
)

// DBStatus 是 note info 对数据库的只读快照；数据库不存在时各计数为零。
type DBStatus struct {
	Exists         bool
	SizeBytes      int64
	SchemaVersion  int
	Notes          int
	EncryptedNotes int
	Categories     int
}

// InfoData 是 FormatInfo 的全部输入，由 cmd/note 采集（构建信息、运行时、配置与数据库快照）。
type InfoData struct {
	Label        string // build.Describe().Label()
	Version      string
	GitHash      string
	GitDirty     bool
	BuildDate    string
	GoVersion    string
	GOOS         string
	GOARCH       string
	Hostname     string
	CPUCount     int
	ConfigPath   string
	ConfigFound  bool
	Addr         string
	AllowRemote  bool
	DBPath       string
	LogPath      string
	LogExists    bool
	LogSizeBytes int64
	DB           DBStatus
}

// FormatInfo 把运行信息渲染成对齐的键值文本（uniseg 计算 CJK 宽度）。
// 与 GET /api/info 同源同口径：只有构建、运行时与配置值，没有密文、盐、口令或令牌。
func FormatInfo(d InfoData) string {
	sections := []struct {
		title string
		rows  [][2]string
	}{
		{"软件", [][2]string{
			{"版本", d.Label},
			{"提交", orDash(gitSummary(d.GitHash, d.GitDirty))},
			{"构建时间", orDash(d.BuildDate)},
			{"Go", d.GoVersion},
		}},
		{"运行环境", [][2]string{
			{"操作系统", d.GOOS + " / " + d.GOARCH},
			{"CPU", strconv.Itoa(d.CPUCount) + " 核"},
			{"主机名", orDash(d.Hostname)},
		}},
		{"配置", [][2]string{
			{"配置文件", d.ConfigPath + configFoundSuffix(d.ConfigFound)},
			{"监听地址", d.Addr},
			{"允许远程", boolText(d.AllowRemote)},
		}},
		{"数据", [][2]string{
			{"数据库", dbSummary(d)},
			{"日志", fileSummary(d.LogPath, d.LogExists, d.LogSizeBytes)},
			{"笔记", notesSummary(d.DB)},
			{"分类", categoriesSummary(d.DB)},
		}},
	}

	labelWidth := 0
	for _, section := range sections {
		for _, row := range section.rows {
			if w := displayWidth(row[0]); w > labelWidth {
				labelWidth = w
			}
		}
	}

	var b strings.Builder
	for _, section := range sections {
		b.WriteString(section.title)
		b.WriteString("\n")
		for _, row := range section.rows {
			fmt.Fprintf(&b, "  %s  %s\n", padRight(row[0], labelWidth), row[1])
		}
		b.WriteString("\n")
	}
	return b.String()
}

func gitSummary(hash string, dirty bool) string {
	if hash == "" {
		return ""
	}
	if dirty {
		return hash + "（含未提交改动）"
	}
	return hash
}

func configFoundSuffix(found bool) string {
	if found {
		return "（已加载）"
	}
	return "（未找到，使用内置默认值）"
}

func dbSummary(d InfoData) string {
	if !d.DB.Exists {
		return d.DBPath + "（未创建）"
	}
	parts := []string{humanSize(d.DB.SizeBytes)}
	if d.DB.SchemaVersion > 0 {
		parts = append(parts, "schema v"+strconv.Itoa(d.DB.SchemaVersion))
	}
	return d.DBPath + "（" + strings.Join(parts, "，") + "）"
}

func notesSummary(db DBStatus) string {
	if !db.Exists {
		return "—"
	}
	if db.EncryptedNotes > 0 {
		return fmt.Sprintf("%d 条（加密 %d 条）", db.Notes, db.EncryptedNotes)
	}
	return strconv.Itoa(db.Notes) + " 条"
}

func categoriesSummary(db DBStatus) string {
	if !db.Exists {
		return "—"
	}
	return strconv.Itoa(db.Categories) + " 个"
}

func boolText(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// humanSize 把字节数渲染成 B/KB/MB（本地单文件场景到 MB 足够）。
func humanSize(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return strconv.FormatInt(n, 10) + " B"
	}
}

// displayWidth 返回终端显示宽度（CJK 字符占两列）。
func displayWidth(s string) int {
	return uniseg.StringWidth(s)
}

// padRight 按显示宽度右侧补空格，使 CJK 标签与 ASCII 标签对齐。
func padRight(s string, width int) string {
	if gap := width - displayWidth(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}
