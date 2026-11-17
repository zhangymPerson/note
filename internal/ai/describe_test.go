package ai

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// attrMap 把 []any 的 key/value 序列摊平成 map，方便断言「这一项到底有没有输出」。
func attrMap(attrs []any) map[string]any {
	out := make(map[string]any, len(attrs)/2)
	for i := 0; i+1 < len(attrs); i += 2 {
		key, ok := attrs[i].(string)
		if !ok {
			continue
		}
		out[key] = attrs[i+1]
	}
	return out
}

func TestDescribeCoversEveryRequestField(t *testing.T) {
	c := &Client{
		BaseURL: "https://gw.example.com/v1/",
		APIKey:  "sk-0123456789abcdefKEY",
		Models:  []string{"m1", "m2"},
		Prompt:  "整理下面这段：\n{{content}}",
	}
	req := Request{Content: "原始正文", CustomPrompt: "用 mermaid"}

	info := c.Describe(req, true)
	got := attrMap(info.Attrs())

	want := map[string]any{
		"endpoint":       "https://gw.example.com/v1/chat/completions",
		"host":           "gw.example.com",
		"base_url":       "https://gw.example.com/v1/",
		"model":          "m1", // 空 model 用列表第一个
		"stream":         true,
		"temperature":    0.2,
		"max_tokens":     maxCompletionTokens,
		"messages":       1,
		"message_role":   "user",
		"prompt_kind":    "custom", // 用了 ai.prompt 模板
		"api_key":        "sk-…fKEY",
		"custom_prompt":  true,
		"http_client":    "shared-stream",
		"proxy_from_env": proxyFromEnv(),
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %v", key, got[key], value)
		}
	}

	// 正文与提示词只能以「长度 + 指纹」出现：日志里不许有原文。
	prompt := BuildPromptWithCustom(c.Prompt, req.Content, req.CustomPrompt)
	if got["prompt_runes"] != len([]rune(prompt)) || got["content_runes"] != len([]rune(req.Content)) {
		t.Errorf("prompt_runes/content_runes = %v/%v", got["prompt_runes"], got["content_runes"])
	}
	if got["prompt_sha256"] != sha12(prompt) || got["content_sha256"] != sha12(req.Content) {
		t.Errorf("指纹对不上：%v / %v", got["prompt_sha256"], got["content_sha256"])
	}
	if got["custom_prompt_runes"] != len([]rune(req.CustomPrompt)) {
		t.Errorf("custom_prompt_runes = %v", got["custom_prompt_runes"])
	}
	for _, attr := range info.Attrs() {
		if text, ok := attr.(string); ok {
			for _, secret := range []string{"原始正文", "用 mermaid", "0123456789abcdef"} {
				if strings.Contains(text, secret) {
					t.Fatalf("日志字段 %q 里出现了不该记的内容（%s）", text, secret)
				}
			}
		}
	}
}

func TestDescribeMarksBuiltinPrompt(t *testing.T) {
	c := &Client{BaseURL: "https://gw.example.com/v1", APIKey: "sk-test-key-123456", Models: []string{"m1"}}
	got := attrMap(c.Describe(Request{Content: "x"}, false).Attrs())

	if got["prompt_kind"] != "builtin" {
		t.Errorf("空 ai.prompt 应标成 builtin，得到 %v", got["prompt_kind"])
	}
	if got["custom_prompt"] != false || got["custom_prompt_runes"] != 0 {
		t.Errorf("没有自定义提示词时应当是 false/0：%v / %v", got["custom_prompt"], got["custom_prompt_runes"])
	}
	if got["http_client"] != "shared" {
		t.Errorf("http_client = %v, want shared", got["http_client"])
	}
	if got["client_timeout_ms"] != int64(300000) {
		t.Errorf("client_timeout_ms = %v, want 300000", got["client_timeout_ms"])
	}
}

func TestDescribeInjectedClientAndStreamTimeoutOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, APIKey: "sk-test-key-123456", Models: []string{"m1"}, HTTP: srv.Client()}
	got := attrMap(c.Describe(Request{Content: "x"}, true).Attrs())
	if got["http_client"] != "injected" {
		t.Errorf("http_client = %v, want injected（测试注入优先于共享实例）", got["http_client"])
	}
	// 流式请求刻意不设总超时：日志必须说清这一点，否则看日志的人会以为超时没用上。
	if got["client_timeout_ms"] != "off" {
		t.Errorf("流式 client_timeout_ms = %v, want off", got["client_timeout_ms"])
	}
}

func TestErrorAttrsExpandsChain(t *testing.T) {
	upstream := UpstreamError{Status: 502, Message: "bad gateway"}
	timeout := TimeoutError{Model: "m1", Duration: 5 * time.Minute, Err: fmt.Errorf("wrapped: %w", upstream)}

	got := attrMap(ErrorAttrs(timeout))
	if got["error_type"] != "ai.TimeoutError" {
		t.Errorf("error_type = %v, want ai.TimeoutError", got["error_type"])
	}
	chain, _ := got["error_chain"].([]string)
	if len(chain) != 3 {
		t.Fatalf("链路应当有三层（Timeout → 包装 → Upstream），得到 %q", chain)
	}
	if !strings.HasPrefix(chain[0], "ai.TimeoutError: ") || !strings.Contains(chain[2], "ai.UpstreamError") {
		t.Errorf("链路顺序不对：%q", chain)
	}
	if !strings.Contains(fmt.Sprint(got["error"]), "5 分钟内没有响应") {
		t.Errorf("error 应当是给人看的那句话：%v", got["error"])
	}
}

func TestErrorAttrsNil(t *testing.T) {
	if attrs := ErrorAttrs(nil); attrs != nil {
		t.Fatalf("nil 错误不该产生字段：%v", attrs)
	}
}

func TestDescribeHandlesEmptyConfig(t *testing.T) {
	// 配置残缺时也不能 panic：日志本身不能成为第二个故障。
	got := attrMap((&Client{}).Describe(Request{}, false).Attrs())
	if got["endpoint"] != "/chat/completions" {
		t.Errorf("endpoint = %v", got["endpoint"])
	}
	if got["api_key"] != "" {
		t.Errorf("api_key = %v, want 空串", got["api_key"])
	}
}

func TestDescribeTimelineReflectsNothingWhenNil(t *testing.T) {
	if attrs := (*Timeline)(nil).Attrs(); attrs != nil {
		t.Fatalf("没有采集时间线时不该输出字段：%v", attrs)
	}
}

func TestErrorChainIsBounded(t *testing.T) {
	// 错误链可能很长（层层包装），日志不能因此爆掉：只留前 5 层。
	var err error = errors.New("根因")
	for i := 0; i < 12; i++ {
		err = fmt.Errorf("层 %d: %w", i, err)
	}
	chain, _ := attrMap(ErrorAttrs(err))["error_chain"].([]string)
	if len(chain) != 5 {
		t.Fatalf("链路应当截断到 5 层，得到 %d 层", len(chain))
	}
}
