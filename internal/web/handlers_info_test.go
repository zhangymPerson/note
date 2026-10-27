package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var utcSecondText = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

func decodeInfo(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var info map[string]any
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("decode info: %v (%s)", err, body)
	}
	return info
}

func TestInfoEndpointReportsBuildAndRuntime(t *testing.T) {
	handler, cfg := newTestHandler(t)

	rec := doRequest(handler, http.MethodGet, "/api/info", "127.0.0.1:8080", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	info := decodeInfo(t, rec.Body.Bytes())
	for _, key := range []string{
		"version", "git_hash", "git_dirty", "build_date", "build_label",
		"go_version", "goos", "goarch", "hostname", "cpu_count",
		"mem_alloc_bytes", "mem_sys_bytes",
		"started_at", "uptime_seconds",
		"db_path", "schema_version", "addr", "allow_remote",
	} {
		if _, ok := info[key]; !ok {
			t.Errorf("response missing field %q: %+v", key, info)
		}
	}

	if got, _ := info["started_at"].(string); !utcSecondText.MatchString(got) {
		t.Errorf("started_at = %q, want UTC 秒级文本 2006-01-02T15:04:05Z", got)
	}
	if got, _ := info["db_path"].(string); got != cfg.DBPath {
		t.Errorf("db_path = %q, want %q", got, cfg.DBPath)
	}
	if got, _ := info["addr"].(string); got != cfg.Addr {
		t.Errorf("addr = %q, want %q", got, cfg.Addr)
	}
	if got, _ := info["schema_version"].(float64); got != 4 {
		t.Errorf("schema_version = %v, want 4（最新迁移 0004）", info["schema_version"])
	}
	if got, _ := info["cpu_count"].(float64); got < 1 {
		t.Errorf("cpu_count = %v, want at least 1", info["cpu_count"])
	}
	// 任何构建方式（含 go test）都必须给出可展示的版本串。
	label, _ := info["build_label"].(string)
	if label == "" {
		t.Errorf("build_label is empty; 开发构建也要有内容")
	}
	if !strings.Contains(label, "1.4.0") {
		t.Errorf("build_label = %q, want it to start from the source version", label)
	}
}

func TestInfoEndpointHasNoSecrets(t *testing.T) {
	handler, _ := newTestHandler(t)

	rec := doRequest(handler, http.MethodGet, "/api/info", "127.0.0.1:8080", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"cipher", "salt", "question"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("响应含 %q；信息页不接触任何加密材料：%s", forbidden, body)
		}
	}
}
