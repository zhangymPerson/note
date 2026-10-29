package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"note/internal/template"
)

type templateResp struct {
	Content        string `json:"content"`
	Path           string `json:"path"`
	Source         string `json:"source"`
	DefaultContent string `json:"default_content"`
}

func decodeTemplate(t *testing.T, body []byte) templateResp {
	t.Helper()
	var out templateResp
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode template response: %v", err)
	}
	return out
}

func TestTemplateAPIFallsBackToBuiltInDefault(t *testing.T) {
	handler, cfg := newTestHandler(t)

	rec := apiRequest(t, handler, http.MethodGet, "/api/template", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := decodeTemplate(t, rec.Body.Bytes())
	if body.Source != "default" || body.Content != template.Default() {
		t.Fatalf("文件缺失时应回退默认: %+v", body)
	}
	if body.Path != filepath.Join(filepath.Dir(cfg.ConfigPath), template.FileName) {
		t.Fatalf("path = %q", body.Path)
	}
	if body.DefaultContent != template.Default() {
		t.Fatal("default_content 应等于内置默认模板")
	}
}

func TestTemplateAPISavesAndReadsBack(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPut, "/api/template", token, `{"content":"# 我的骨架\n\n- 一\n- 二"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d, want 200: %s", rec.Code, rec.Body)
	}
	if body := decodeTemplate(t, rec.Body.Bytes()); body.Source != "file" || !strings.Contains(body.Content, "我的骨架") {
		t.Fatalf("PUT 响应 = %+v", body)
	}

	// 落盘位置与内容
	data, err := os.ReadFile(filepath.Join(filepath.Dir(cfg.ConfigPath), template.FileName))
	if err != nil || !strings.Contains(string(data), "我的骨架") {
		t.Fatalf("文件内容 = %q, %v", data, err)
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/template", "", "")
	if body := decodeTemplate(t, rec.Body.Bytes()); body.Source != "file" || !strings.Contains(body.Content, "我的骨架") {
		t.Fatalf("GET 读回 = %+v", body)
	}
}

func TestTemplateAPIRejectsTooLongContent(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	tooLong := strings.Repeat("字", maxContentRunes+1)
	payload, err := json.Marshal(map[string]string{"content": tooLong})
	if err != nil {
		t.Fatal(err)
	}
	rec := apiRequest(t, handler, http.MethodPut, "/api/template", token, string(payload))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT 超长 = %d, want 400: %s", rec.Code, rec.Body)
	}
	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "validation_failed" || body.Error.Fields["content"] == "" {
		t.Fatalf("错误体 = %+v", body.Error)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.ConfigPath), template.FileName)); !os.IsNotExist(err) {
		t.Fatal("校验失败时不该写文件")
	}
}

func TestTemplateAPIAllowsEmptyContent(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPut, "/api/template", token, `{"content":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT 空内容 = %d, want 200: %s", rec.Code, rec.Body)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(cfg.ConfigPath), template.FileName))
	if err != nil || len(data) != 0 {
		t.Fatalf("空模板应写成 0 字节文件: %q, %v", data, err)
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/template", "", "")
	if body := decodeTemplate(t, rec.Body.Bytes()); body.Source != "file" || body.Content != "" {
		t.Fatalf("空模板 GET = %+v", body)
	}
}

func TestTemplateAPIRequiresCSRFForWrites(t *testing.T) {
	handler, _ := newTestHandler(t)

	rec := apiRequest(t, handler, http.MethodPut, "/api/template", "", `{"content":"# x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无令牌 PUT = %d, want 403: %s", rec.Code, rec.Body)
	}
	if rec := apiRequest(t, handler, http.MethodGet, "/api/template", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET 不需要 CSRF，得到 %d", rec.Code)
	}
}
