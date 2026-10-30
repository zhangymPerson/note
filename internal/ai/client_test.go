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

	res, err := c.Optimize(context.Background(), "原始正文", "")
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

func TestOptimizeRejectsUnknownModel(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("模型不在列表时不该发请求")
	})
	_, err := c.Optimize(context.Background(), "x", "nope")
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
	_, err := c.Optimize(context.Background(), "x", "")
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
			if _, err := c.Optimize(context.Background(), "x", ""); err == nil {
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
	if _, err := c.Optimize(ctx, "x", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("应当返回 ctx 错误，得到 %v", err)
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
