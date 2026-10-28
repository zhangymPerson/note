package cli

import (
	"strings"
	"testing"
)

func sampleInfoData() InfoData {
	return InfoData{
		Label:       "1.4.0（开发构建）",
		Version:     "1.4.0",
		GitHash:     "9c0362f",
		GitDirty:    true,
		BuildDate:   "2026-10-28T06:00:00Z",
		GoVersion:   "go1.27.0",
		GOOS:        "windows",
		GOARCH:      "amd64",
		Hostname:    "THINKPAD",
		CPUCount:    8,
		ConfigPath:  `C:\Users\me\AppData\Roaming\note\note.yaml`,
		ConfigFound: true,
		Addr:        "127.0.0.1:8080",
		AllowRemote: false,
		DBPath:      `C:\Users\me\AppData\Roaming\note\notes.db`,
		DB: DBStatus{
			Exists:         true,
			SizeBytes:      1536 * 1024,
			SchemaVersion:  5,
			Notes:          12,
			EncryptedNotes: 3,
			Categories:     4,
		},
	}
}

func TestFormatInfoContainsAllSections(t *testing.T) {
	out := FormatInfo(sampleInfoData())
	for _, want := range []string{
		"软件", "运行环境", "配置", "数据",
		"1.4.0（开发构建）",
		"9c0362f（含未提交改动）",
		"go1.27.0",
		"windows / amd64",
		"8 核",
		"127.0.0.1:8080",
		"（已加载）",
		"1.5 MB",
		"schema v5",
		"12 条（加密 3 条）",
		"4 个",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("输出缺少 %q:\n%s", want, out)
		}
	}
}

func TestFormatInfoMissingPieces(t *testing.T) {
	d := sampleInfoData()
	d.GitHash = ""
	d.Hostname = ""
	d.ConfigFound = false
	d.DB = DBStatus{Exists: false}

	out := FormatInfo(d)
	for _, want := range []string{
		"（未找到，使用内置默认值）",
		"（未创建）",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("输出缺少 %q:\n%s", want, out)
		}
	}
	// 数据库未创建时笔记/分类行显示占位符
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "笔记") && !strings.Contains(line, "—") {
			t.Fatalf("数据库未创建时笔记行应为占位符: %q", line)
		}
	}
}

func TestFormatInfoAlignsCJKLabels(t *testing.T) {
	out := FormatInfo(sampleInfoData())

	// 每个键值行的值列必须从同一显示列开始：按显示宽度解析每行。
	var valueColumns []int
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "  ") || strings.TrimSpace(line) == "" {
			continue
		}
		trimmed := strings.TrimPrefix(line, "  ")
		firstSpace := strings.Index(trimmed, " ")
		if firstSpace <= 0 {
			t.Fatalf("键值行缺少标签与值的分隔: %q", line)
		}
		value := strings.TrimLeft(trimmed[firstSpace:], " ")
		if value == "" {
			t.Fatalf("键值行为空值: %q", line)
		}
		valueColumns = append(valueColumns, displayWidth(trimmed[:len(trimmed)-len(value)]))
	}
	if len(valueColumns) < 10 {
		t.Fatalf("键值行数量不足: %d\n%s", len(valueColumns), out)
	}
	for _, col := range valueColumns {
		if col != valueColumns[0] {
			t.Fatalf("值列未对齐: %v\n%s", valueColumns, out)
		}
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{512, "512 B"},
		{2048, "2.0 KB"},
		{1536 * 1024, "1.5 MB"},
	}
	for _, tc := range cases {
		if got := humanSize(tc.in); got != tc.want {
			t.Fatalf("humanSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
