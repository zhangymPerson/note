package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseUpstream 是流式上游的替身：按给定片段依次吐 SSE 行。
// 它同时记录上游收到的请求体，用来确认 stream:true 真的发出去了。
func sseUpstream(t *testing.T, chunks ...string) (*httptest.Server, *map[string]any) {
	t.Helper()

	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("请求体不是 JSON: %v (%s)", err, raw)
		}
		got = body

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, chunk := range chunks {
			io.WriteString(w, chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func sseDelta(text string) string {
	raw, _ := json.Marshal(text)
	return `data: {"choices":[{"delta":{"content":` + string(raw) + `},"finish_reason":null}]}` + "\n\n"
}

// parseSSE 把响应体切成 [(event, data)]。
func parseSSE(t *testing.T, body string) [][2]string {
	t.Helper()

	var events [][2]string
	for _, block := range strings.Split(body, "\n\n") {
		var name, data string
		for _, line := range strings.Split(block, "\n") {
			if value, ok := strings.CutPrefix(line, "event: "); ok {
				name = value
			}
			if value, ok := strings.CutPrefix(line, "data: "); ok {
				data = value
			}
		}
		if name != "" {
			events = append(events, [2]string{name, data})
		}
	}
	return events
}

func TestOptimizeAPIStreamForwardsDeltas(t *testing.T) {
	handler, cfg := newTestHandler(t)
	upstream, sent := sseUpstream(t,
		sseDelta("```markdown\n"),
		sseDelta("# 标题\n\n"),
		sseDelta("正文\n```"),
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n",
		"data: [DONE]\n\n",
	)
	configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n")
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"原始正文","stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	// 流里就是正文，必须禁止缓存（与普通响应同口径）。
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	// 反向代理默认会攒满缓冲再转发，这一句让「边生成边看」真的成立。
	if got := rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", got)
	}

	if stream, _ := (*sent)["stream"].(bool); !stream {
		t.Errorf("上游收到 stream = %v, want true", (*sent)["stream"])
	}

	events := parseSSE(t, rec.Body.String())
	var deltas []string
	var done map[string]string
	for _, event := range events {
		switch event[0] {
		case "delta":
			var payload struct{ Text string }
			if err := json.Unmarshal([]byte(event[1]), &payload); err != nil {
				t.Fatalf("delta 不是 JSON: %v (%s)", err, event[1])
			}
			deltas = append(deltas, payload.Text)
		case "done":
			if err := json.Unmarshal([]byte(event[1]), &done); err != nil {
				t.Fatalf("done 不是 JSON: %v (%s)", err, event[1])
			}
		case "error":
			t.Fatalf("不该有 error 事件: %s", event[1])
		}
	}
	if want := []string{"```markdown\n", "# 标题\n\n", "正文\n```"}; strings.Join(deltas, "|") != strings.Join(want, "|") {
		t.Fatalf("delta 序列 = %q, want %q", deltas, want)
	}
	if done["content"] != "# 标题\n\n正文" || done["model"] != "m1" {
		t.Fatalf("done = %+v", done)
	}
	// 只有在收到上游完整结果后才发 done：它是「可以替换」的唯一信号。
	if events[len(events)-1][0] != "done" {
		t.Fatalf("最后一个事件必须是 done，得到 %v", events[len(events)-1])
	}
	if strings.Contains(rec.Body.String(), "sk-test") {
		t.Fatal("事件流里不许出现密钥")
	}
}

func TestOptimizeAPIStreamReportsUpstreamError(t *testing.T) {
	handler, cfg := newTestHandler(t)
	// 上游在流开始后才失败（4xx 直接回，没有 SSE 头）。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	t.Cleanup(upstream.Close)
	configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n")
	token := csrfTokenFor(t, handler)

	// 还没开始写事件流时，错误按普通 JSON 回：前端能照旧按 ApiRequestError 处理。
	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"c","stream":true}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "rate limited") {
		t.Fatalf("上游摘要应当透出: %s", rec.Body)
	}
}

func TestOptimizeAPIStreamTruncationEvent(t *testing.T) {
	handler, cfg := newTestHandler(t)
	// 已经吐出过 delta 之后才发现被截断：这时不能再改状态码，只能发 error 事件。
	upstream, _ := sseUpstream(t,
		sseDelta("写到一半"),
		`data: {"choices":[{"delta":{},"finish_reason":"length"}]}`+"\n\n",
	)
	configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n")
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"c","stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("已经开始流式响应，状态码必须还是 200: %d", rec.Code)
	}
	events := parseSSE(t, rec.Body.String())
	last := events[len(events)-1]
	if last[0] != "error" {
		t.Fatalf("截断必须发 error 事件，得到 %v", last)
	}
	var payload struct{ Code, Message string }
	if err := json.Unmarshal([]byte(last[1]), &payload); err != nil {
		t.Fatalf("error 事件不是 JSON: %v (%s)", err, last[1])
	}
	if payload.Code != "ai_truncated" || !strings.Contains(payload.Message, "上限") {
		t.Fatalf("error = %+v", payload)
	}
}

func TestOptimizeAPIStreamValidationBeforeStreaming(t *testing.T) {
	handler, cfg := newTestHandler(t)
	configureAI(t, cfg, "http://127.0.0.1:1/v1", "    - m1\n")
	token := csrfTokenFor(t, handler)

	// 校验失败时不能先开 SSE：否则前端拿不到 400 与字段级提示。
	for name, payload := range map[string]string{
		"空正文":  `{"content":"  ","stream":true}`,
		"未知模型": `{"content":"c","model":"nope","stream":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, payload)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d: %s", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
				t.Fatalf("Content-Type = %q, want JSON 错误体", got)
			}
		})
	}
}
