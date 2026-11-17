package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseServer 是流式上游的替身：按给定片段依次写 SSE data 行并立即 flush，
// 顺便把请求体记下来（校验 stream / max_tokens 是不是真的发出去了）。
func sseServer(t *testing.T, chunks ...string) (*Client, *httptest.Server, *map[string]any) {
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

	return &Client{
		BaseURL: srv.URL + "/v1",
		APIKey:  "sk-test",
		Models:  []string{"m1", "m2"},
		HTTP:    srv.Client(),
		Prompt:  "整理：\n" + "{{content}}",
	}, srv, &got
}

// delta 是一段正规的流式增量。
func delta(text string) string {
	return `data: {"choices":[{"delta":{"content":` + quote(text) + `},"finish_reason":null}]}` + "\n\n"
}

func quote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func TestOptimizeStreamForwardsDeltasAndStripsFence(t *testing.T) {
	c, _, payload := sseServer(t,
		delta("```markdown\n"),
		delta("# 标题\n\n"),
		delta("正文\n```"),
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n",
		"data: [DONE]\n\n",
	)

	var got []string
	res, err := c.OptimizeStream(context.Background(), Request{Content: "原始正文"}, func(text string) error {
		got = append(got, text)
		return nil
	})
	if err != nil {
		t.Fatalf("OptimizeStream: %v", err)
	}
	if want := []string{"```markdown\n", "# 标题\n\n", "正文\n```"}; !equalStrings(got, want) {
		t.Fatalf("增量回调 = %q, want %q", got, want)
	}
	if res.Content != "# 标题\n\n正文" {
		t.Fatalf("结果应当剥掉整段围栏，得到 %q", res.Content)
	}
	if res.Model != "m1" {
		t.Fatalf("Model = %q, want m1（默认第一个模型）", res.Model)
	}

	// 流式请求必须显式要求上游边生成边返回，并带上输出上限。
	if stream, _ := (*payload)["stream"].(bool); !stream {
		t.Errorf("payload stream = %v, want true", (*payload)["stream"])
	}
	if maxTokens, _ := (*payload)["max_tokens"].(float64); int(maxTokens) != maxCompletionTokens {
		t.Errorf("payload max_tokens = %v, want %d", (*payload)["max_tokens"], maxCompletionTokens)
	}
}

func TestOptimizeStreamReportsTruncation(t *testing.T) {
	c, _, _ := sseServer(t,
		delta("写到一半"),
		`data: {"choices":[{"delta":{},"finish_reason":"length"}]}`+"\n\n",
	)

	_, err := c.OptimizeStream(context.Background(), Request{Content: "正文"}, nil)
	var truncated TruncatedError
	if !errors.As(err, &truncated) {
		t.Fatalf("finish_reason=length 必须报截断，得到 %v", err)
	}
	if !strings.Contains(truncated.Error(), "m1") {
		t.Errorf("文案里应当带上模型名：%q", truncated.Error())
	}
}

func TestOptimizeStreamIgnoresNoise(t *testing.T) {
	// 心跳注释行、坏 JSON 行都不该毁掉整次生成，也不该混进正文。
	c, _, _ := sseServer(t,
		": keep-alive\n\n",
		"data: {not json}\n\n",
		delta("正文"),
		"\n",
		"data: [DONE]\n\n",
	)

	var got []string
	res, err := c.OptimizeStream(context.Background(), Request{Content: "正文"}, func(text string) error {
		got = append(got, text)
		return nil
	})
	if err != nil {
		t.Fatalf("OptimizeStream: %v", err)
	}
	if res.Content != "正文" || len(got) != 1 {
		t.Fatalf("噪声行必须被忽略，得到 content=%q deltas=%q", res.Content, got)
	}
}

func TestOptimizeStreamFallsBackToWholeJSON(t *testing.T) {
	// 有些网关（或带流式开关的代理）会无视 stream:true 直接回整段 JSON：
	// 这时按非流式解析，不要报「空结果」。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":`+quote("```\n# 标题\n```")+`},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Models: []string{"m1"}, HTTP: srv.Client()}

	var got []string
	res, err := c.OptimizeStream(context.Background(), Request{Content: "正文"}, func(text string) error {
		got = append(got, text)
		return nil
	})
	if err != nil {
		t.Fatalf("OptimizeStream: %v", err)
	}
	if res.Content != "# 标题" {
		t.Fatalf("Content = %q, want # 标题", res.Content)
	}
	if want := []string{"```\n# 标题\n```"}; !equalStrings(got, want) {
		t.Fatalf("回落路径也应当把整段内容交给回调，得到 %q", got)
	}
}

func TestOptimizeStreamStopsWhenCallbackFails(t *testing.T) {
	c, srv, _ := sseServer(t, delta("第一段"), delta("第二段"))

	stop := errors.New("用户点了停止")
	calls := 0
	_, err := c.OptimizeStream(context.Background(), Request{Content: "正文"}, func(string) error {
		calls++
		if calls == 1 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("回调报错必须中止调用，得到 %v", err)
	}
	if calls != 1 {
		t.Fatalf("中止后不该再回调，调用次数 %d", calls)
	}
	_ = srv
}

func TestOptimizeStreamMapsUpstreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Models: []string{"m1"}, HTTP: srv.Client()}

	_, err := c.OptimizeStream(context.Background(), Request{Content: "正文"}, nil)
	var upstream UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != http.StatusTooManyRequests {
		t.Fatalf("want UpstreamError 429, got %v", err)
	}
	if !strings.Contains(upstream.Error(), "rate limited") {
		t.Errorf("错误摘要应当带上上游原文：%q", upstream.Error())
	}
}

func TestOptimizeStreamRejectsUnknownModel(t *testing.T) {
	c, _, _ := sseServer(t)
	_, err := c.OptimizeStream(context.Background(), Request{Content: "正文", Model: "nope"}, nil)
	var modelErr ModelError
	if !errors.As(err, &modelErr) {
		t.Fatalf("want ModelError, got %v", err)
	}
}

func TestOptimizeStreamEmptyWhenNoContent(t *testing.T) {
	c, _, _ := sseServer(t, "data: [DONE]\n\n")
	_, err := c.OptimizeStream(context.Background(), Request{Content: "正文"}, nil)
	if !errors.Is(err, ErrEmptyResult) {
		t.Fatalf("want ErrEmptyResult, got %v", err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// 保证 fmt 在本文件里真的被用到（错误信息拼接用）。
var _ = fmt.Sprintf
