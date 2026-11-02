package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"note/internal/ai"
	"note/internal/config"
)

// fakeUpstream 是 OpenAI 兼容接口的替身：只回一条 chat/completions 结果。
// 它同时校验上游收到的请求形状（一条 user 消息）。注意这里解码是大小写不敏感的，
// 所以「键名必须是全小写」只能由 internal/ai 的原始 JSON 用例来钉。
func fakeUpstream(t *testing.T, content string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		raw, err := io.ReadAll(r.Body)
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err != nil || json.Unmarshal(raw, &body) != nil {
			t.Errorf("请求体不是 JSON: %v (%s)", err, raw)
		} else if len(body.Messages) != 1 || body.Messages[0].Role != "user" || body.Messages[0].Content == "" {
			t.Errorf("上游收到的 messages 不符合 chat/completions 形状: %s", raw)
		}
		io.WriteString(w, `{"choices":[{"message":{"content":`+strconv.Quote(content)+`}}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// configureAI 直接把 AI 配置写进 note.yaml：这个端点每次请求都重新读盘。
func configureAI(t *testing.T, cfg config.Config, baseURL, models string) {
	t.Helper()
	data := "ai:\n  base_url: " + baseURL + "\n  api_key: sk-test\n"
	if models != "" {
		data += "  models:\n" + models
	}
	if err := os.MkdirAll(filepath.Dir(cfg.ConfigPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cfg.ConfigPath, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestOptimizeAPIReturnsContentAndModel(t *testing.T) {
	handler, cfg := newTestHandler(t)
	upstream := fakeUpstream(t, "```markdown\n# 标题\n```")
	configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n    - m2\n")
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"原始正文"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("含正文的响应必须 no-store")
	}
	var body struct{ Content, Model string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body)
	}
	if body.Content != "# 标题" || body.Model != "m1" {
		t.Fatalf("body = %+v", body)
	}
	if strings.Contains(rec.Body.String(), "sk-test") {
		t.Fatal("响应里不许出现密钥")
	}
}

func TestOptimizeAPIModelOverride(t *testing.T) {
	handler, cfg := newTestHandler(t)
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Model string }
		json.NewDecoder(r.Body).Decode(&b)
		got = b.Model
		io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
	}))
	t.Cleanup(upstream.Close)
	configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n    - m2\n")
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"c","model":"m2"}`)
	if rec.Code != http.StatusOK || got != "m2" {
		t.Fatalf("code=%d model=%q", rec.Code, got)
	}

	rec = apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"c","model":"nope"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"model"`) {
		t.Fatalf("未知模型应当 400 字段级: %d %s", rec.Code, rec.Body)
	}
}

func TestOptimizeAPINotConfigured(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"c"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "ai_not_configured") {
		t.Fatalf("未配置应当 409: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "ai.models") {
		t.Fatalf("文案要点名缺哪几项: %s", rec.Body)
	}
}

func TestOptimizeAPIValidation(t *testing.T) {
	handler, cfg := newTestHandler(t)
	configureAI(t, cfg, "http://127.0.0.1:1/v1", "    - m1\n")
	token := csrfTokenFor(t, handler)

	cases := map[string]string{
		"空正文":  `{"content":"   "}`,
		"超长正文": `{"content":"` + strings.Repeat("字", 2001) + `"}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			// 校验必须发生在发请求之前：这里的上游是不可达地址
			rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, payload)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d: %s", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), `"content"`) {
				t.Fatalf("应当是字段级 content 错误: %s", rec.Body)
			}
		})
	}
}

func TestOptimizeAPIUpstreamFailure(t *testing.T) {
	handler, cfg := newTestHandler(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	t.Cleanup(upstream.Close)
	configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n")
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"c"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("上游摘要应当透出: %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "sk-test") {
		t.Fatal("上游错误里不许带密钥")
	}
}

func TestOptimizeErrorTimeoutIsActionable(t *testing.T) {
	rec := httptest.NewRecorder()
	writeOptimizeError(rec, ai.TimeoutError{
		Model:    "glm-5.3-flash",
		Duration: 60 * time.Second,
		Err:      context.DeadlineExceeded,
	})

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("超时应当是 504，得到 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"ai_timeout", "glm-5.3-flash", "60 秒", "重试"} {
		if !strings.Contains(body, want) {
			t.Fatalf("文案缺 %q：%s", want, body)
		}
	}
}

func TestOptimizeAPIRequiresCSRF(t *testing.T) {
	handler, _ := newTestHandler(t)

	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", "", `{"content":"c"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无 CSRF 应当 403，得到 %d", rec.Code)
	}
}
