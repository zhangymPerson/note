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
	"time"
	"unicode/utf8"

	"note/internal/config"
)

// 所有用例都走注入的 HTTP 客户端与 httptest，不碰真网络：
// 这个包是进程里唯一对外的出口，测试必须能证明「发什么、收什么、怎么失败」。
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{
		BaseURL: srv.URL + "/v1",
		APIKey:  "sk-test",
		Models:  []string{"m1", "m2"},
		HTTP:    srv.Client(),
	}, srv
}

func TestOptimizeSendsRenderedPrompt(t *testing.T) {
	var gotPath, gotAuth, gotModel, gotMessage string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		var body struct {
			Model    string
			Messages []struct{ Role, Content string }
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotModel, gotMessage = body.Model, body.Messages[0].Content
		io.WriteString(w, fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, "```markdown\n# 标题\n```"))
	})
	c.Prompt = "把这段整理好：\n" + config.PromptVariable

	res, err := c.Optimize(context.Background(), Request{Content: "原始正文"})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotModel != "m1" {
		t.Fatalf("默认应当用第一个模型，得到 %q", gotModel)
	}
	if gotMessage != "把这段整理好：\n原始正文" {
		t.Fatalf("提示词未渲染: %q", gotMessage)
	}
	if res.Content != "# 标题" {
		t.Fatalf("围栏应当剥掉: %q", res.Content)
	}
	if res.Model != "m1" {
		t.Fatalf("res.Model = %q", res.Model)
	}
}

// 线上对端是真正的 OpenAI 兼容服务，键名大小写敏感：必须发 role/content 而不是
// Go 结构体字段名。解码用无 tag 的结构体会自动忽略大小写，看不出问题，所以这里
// 直接断言原始 JSON 的键。
func TestOptimizeSendsLowercaseMessageKeys(t *testing.T) {
	var raw []byte
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	})
	c.Prompt = "整理：" + config.PromptVariable

	if _, err := c.Optimize(context.Background(), Request{Content: "原始正文"}); err != nil {
		t.Fatalf("Optimize: %v", err)
	}

	var payload struct {
		Model       string              `json:"model"`
		Temperature float64             `json:"temperature"`
		Stream      *bool               `json:"stream"`
		Messages    []map[string]string `json:"messages"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("请求体不是 JSON: %v (%s)", err, raw)
	}
	if payload.Model != "m1" {
		t.Errorf("model = %q", payload.Model)
	}
	if payload.Temperature != 0.2 {
		t.Errorf("temperature = %v", payload.Temperature)
	}
	if payload.Stream == nil || *payload.Stream {
		t.Errorf("stream 必须是显式 false")
	}
	if len(payload.Messages) != 1 {
		t.Fatalf("messages = %v", payload.Messages)
	}
	if payload.Messages[0]["role"] != "user" {
		t.Errorf("role 键必须是全小写，得到 %v", payload.Messages[0])
	}
	if payload.Messages[0]["content"] != "整理：原始正文" {
		t.Errorf("content = %q", payload.Messages[0]["content"])
	}
	if len(payload.Messages[0]) != 2 {
		t.Errorf("messages[0] 只该有 role 与 content，得到 %v", payload.Messages[0])
	}
}

func TestOptimizeRejectsUnknownModel(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("模型不在列表时不该发请求")
	})
	_, err := c.Optimize(context.Background(), Request{Content: "x", Model: "nope"})
	var bad ModelError
	if !errors.As(err, &bad) || bad.Model != "nope" {
		t.Fatalf("err = %v", err)
	}
}

func TestOptimizeUpstreamErrorKeepsStatusAndTruncates(t *testing.T) {
	long := strings.Repeat("坏", 500)
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"error":{"message":%q}}`, long)
	})
	_, err := c.Optimize(context.Background(), Request{Content: "x"})
	var up UpstreamError
	if !errors.As(err, &up) {
		t.Fatalf("err = %v", err)
	}
	if up.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d", up.Status)
	}
	if n := utf8.RuneCountInString(up.Message); n > 200 {
		t.Fatalf("摘要 %d 字，应当截断到 200", n)
	}
}

func TestOptimizeFailsOnBadJSONEmptyChoicesAndOversize(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"坏 JSON", `not json`},
		{"没有 choices", `{"choices":[]}`},
		{"正文为空", `{"choices":[{"message":{"content":"   \n"}}]}`},
		{"响应过大", `{"choices":[{"message":{"content":"` + strings.Repeat("x", 1<<20) + `"}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, tc.body)
			})
			if _, err := c.Optimize(context.Background(), Request{Content: "x"}); err == nil {
				t.Fatal("应当报错")
			}
		})
	}
}

func TestOptimizeRespectsContextAndTrailingSlash(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		time.Sleep(200 * time.Millisecond) // 让 ctx 先到期
		io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
	})
	c.BaseURL += "/" // 末尾斜杠不许拼出 //
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Optimize(ctx, Request{Content: "x"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("应当返回 ctx 错误，得到 %v", err)
	}
}

func TestOptimizeWrapsTimeoutWithActionableMessage(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
	})
	c.HTTP = &http.Client{Timeout: 20 * time.Millisecond}

	_, err := c.Optimize(context.Background(), Request{Content: "x"})
	if err == nil {
		t.Fatal("应当超时")
	}
	var timeoutErr TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("应当是 TimeoutError，得到 %T: %v", err, err)
	}
	if timeoutErr.Model != "m1" || timeoutErr.Duration != 20*time.Millisecond {
		t.Fatalf("TimeoutError 要带模型与时长: %+v", timeoutErr)
	}
	// 原始错误链不能断：web 层靠 errors.Is 判 504。
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("应当仍可 Is DeadlineExceeded: %v", err)
	}
	for _, want := range []string{"模型 m1", "20 毫秒", "重试", "换一个模型"} {
		if !strings.Contains(timeoutErr.Error(), want) {
			t.Fatalf("超时文案缺 %q: %s", want, timeoutErr.Error())
		}
	}
	if strings.Contains(timeoutErr.Error(), `Post "`) {
		t.Fatalf("别把 net/http 的原文甩给用户: %s", timeoutErr.Error())
	}
}

func TestBuildPrompt(t *testing.T) {
	if BuildPrompt("", "笔记") != strings.Replace(config.DefaultAIPrompt, config.PromptVariable, "笔记", 1) {
		t.Fatal("空模板应当使用默认提示词")
	}
	if got := BuildPrompt("前缀"+config.PromptVariable, "含 {{content}} 的正文"); got != "前缀含 {{content}} 的正文" {
		t.Fatalf("正文里的变量不许二次展开: %q", got)
	}
	if strings.Count(BuildPrompt("x"+config.PromptVariable, "{{content}} {{content}}"), "{{content}}") != 2 {
		t.Fatal("正文原样保留")
	}
}

func TestBuildPromptWithCustom(t *testing.T) {
	tpl := "整理：" + config.PromptVariable
	base := BuildPrompt(tpl, "笔记")

	// 没有额外要求（或只有空白）时与 BuildPrompt 完全一致：不多一条空标题
	if got := BuildPromptWithCustom(tpl, "笔记", "  \n "); got != base {
		t.Fatalf("无额外要求时应当原样返回: %q", got)
	}

	got := BuildPromptWithCustom(tpl, "笔记", "  用 mermaid 图表展示  ")
	if !strings.HasPrefix(got, base) || !strings.HasSuffix(got, "用 mermaid 图表展示") {
		t.Fatalf("额外要求应当追加在模板之后: %q", got)
	}
	// 「只输出正文」「不超篇幅」这些全局约束不能被补充要求冲掉
	for _, want := range []string{"满足上面全部要求", "只输出优化后的 Markdown 正文"} {
		if !strings.Contains(got, want) {
			t.Fatalf("缺少约束 %q: %q", want, got)
		}
	}
	// 额外要求里的 {{content}} 同样不展开：正文只在前面的模板里化入一次
	if strings.Count(BuildPromptWithCustom(tpl, "笔记", "{{content}}"), "{{content}}") != 1 {
		t.Fatal("额外要求里的变量不该被展开")
	}
}

func TestValidateCustomPrompt(t *testing.T) {
	if err := ValidateCustomPrompt(""); err != nil {
		t.Fatalf("空串是合法的: %v", err)
	}
	if err := ValidateCustomPrompt(strings.Repeat("字", MaxCustomPromptRunes)); err != nil {
		t.Fatalf("上限内应当通过: %v", err)
	}
	err := ValidateCustomPrompt(strings.Repeat("字", MaxCustomPromptRunes+1))
	if err == nil || !strings.Contains(err.Error(), "自定义提示词") {
		t.Fatalf("超长应当拒绝并点名该字段: %v", err)
	}
}

func TestOptimizeSendsCustomPrompt(t *testing.T) {
	var gotMessage string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Content string }
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotMessage = body.Messages[0].Content
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	})
	c.Prompt = "整理：" + config.PromptVariable

	_, err := c.Optimize(context.Background(), Request{
		Content:      "原始正文",
		CustomPrompt: "  用 mermaid 图表展示  ",
	})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if !strings.Contains(gotMessage, "整理：原始正文") || !strings.Contains(gotMessage, "用 mermaid 图表展示") {
		t.Fatalf("自定义提示词没发出去: %q", gotMessage)
	}
	if strings.Count(gotMessage, "原始正文") != 1 {
		t.Fatalf("正文只该出现一次: %q", gotMessage)
	}
}

func TestStripFence(t *testing.T) {
	cases := []struct{ in, want string }{
		{"```markdown\n# 标题\n```", "# 标题"},
		{"~~~\n# 标题\n~~~", "# 标题"},
		{"```\n# 标题\n```\n", "# 标题"},
		{"# 标题", "# 标题"},
		{"```\n一\n```\n```\n二\n```", "```\n一\n```\n```\n二\n```"}, // 多段围栏不剥
	}
	for _, c := range cases {
		if got := StripFence(c.in); got != c.want {
			t.Errorf("StripFence(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestConfiguredAndMissing(t *testing.T) {
	c := NewClient(config.AI{})
	if c.Configured() {
		t.Fatal("空配置不该算已配置")
	}
	if got := strings.Join(c.Missing(), ","); got != "ai.base_url,ai.api_key,ai.models" {
		t.Fatalf("missing = %s", got)
	}
	full := NewClient(config.AI{BaseURL: "https://api.example.com/v1", APIKey: "k", Models: []string{"m"}})
	if !full.Configured() || full.DefaultModel() != "m" {
		t.Fatal("完整配置")
	}
	if full.Host() != "api.example.com" {
		t.Fatalf("host = %q", full.Host())
	}
	if got := NewClient(config.AI{BaseURL: "不是地址"}).Host(); got != "不是地址" {
		t.Fatalf("解析失败时应当原样返回，得到 %q", got)
	}
}
