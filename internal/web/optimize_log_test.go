package web

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureLogs 把默认 logger 临时换成写进 buffer 的文本 handler，
// 用于断言「失败时到底打了什么」。测试结束自动还原。
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// 上游 500：这是最常见的「模型报错」，日志必须把入参一次打全。
func TestOptimizeFailureLogsEveryRequestField(t *testing.T) {
	logs := captureLogs(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"model overloaded"}}`))
	}))
	t.Cleanup(upstream.Close)

	handler, cfg := newTestHandler(t)
	configureAI(t, cfg, upstream.URL, "    - m1\n    - m2\n")
	token := csrfTokenFor(t, handler)

	body := `{"content":"# 机密正文\n\n不要出现在日志里","model":"m1","custom_prompt":"用 mermaid 展示"}`
	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, body)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", rec.Code, rec.Body)
	}

	line := logs.String()
	// 「每一项入参都输出来」：逐个断言关键字段在场。
	for _, want := range []string{
		`msg="optimize note failed"`,
		"endpoint=" + upstream.URL + "/chat/completions",
		"host=127.0.0.1:",
		"model=m1",
		"stream=false",
		"temperature=0.2",
		"max_tokens=3000",
		"messages=1",
		"message_role=user",
		"prompt_kind=builtin",
		"prompt_runes=",
		"prompt_bytes=",
		"prompt_sha256=",
		"content_runes=",
		"content_sha256=",
		"custom_prompt=true",
		"custom_prompt_runes=",
		"custom_prompt_sha256=",
		"api_key=", // 只出现掩码（短密钥就是 8 个星号），绝不是明文
		"http_client=shared",
		"client_timeout_ms=300000",
		"elapsed_ms=",
		"deadline_in_ms=",
		"client_gone=false",
		"first_byte_ms=",
		"error_type=ai.UpstreamError",
		"error_chain=",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("日志缺少 %q\n%s", want, line)
		}
	}

	// 硬约束：正文、自定义提示词、api_key 明文一个字都不能进日志。
	for _, secret := range []string{"机密正文", "不要出现在日志里", "用 mermaid 展示", "sk-test"} {
		if strings.Contains(line, secret) {
			t.Fatalf("日志泄露了 %q\n%s", secret, line)
		}
	}
}

// 流式失败额外带上 SSE 进度：能分清「一个 delta 都没出」还是「写到一半断了」。
func TestOptimizeStreamFailureLogsProgress(t *testing.T) {
	logs := captureLogs(t)

	// 上游吐一段就 500？不行——状态码在建流前就决定了。这里让上游直接 502，
	// 于是走「开流前失败」分支，日志照样要带 stream=true 与 first_delta_ms=off。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream boom"}}`))
	}))
	t.Cleanup(upstream.Close)

	handler, cfg := newTestHandler(t)
	configureAI(t, cfg, upstream.URL, "    - m1\n")
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token,
		`{"content":"机密正文","stream":true}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", rec.Code, rec.Body)
	}

	line := logs.String()
	for _, want := range []string{
		`msg="optimize note stream failed"`,
		"stream=true",
		"http_client=shared-stream",
		"client_timeout_ms=off", // 流式不设总超时，日志必须说明
		"response_started=false",
		"deltas=0",
		"first_delta_ms=off",
		"error_type=ai.UpstreamError",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("日志缺少 %q\n%s", want, line)
		}
	}
	if strings.Contains(line, "机密正文") {
		t.Fatalf("日志泄露了正文\n%s", line)
	}
}
