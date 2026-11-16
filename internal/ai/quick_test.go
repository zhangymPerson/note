package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestQuickNoteSendsBuiltinPrompt(t *testing.T) {
	var gotPath, gotAuth, gotModel, gotMessage string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		var body struct {
			Model    string
			Messages []struct{ Role, Content string }
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotModel, gotMessage = body.Model, body.Messages[0].Content
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"title\":\"待办\",\"content\":\"- 买牛奶\"}"}}]}`)
	})

	res, err := c.QuickNote(context.Background(), Request{Content: "买牛奶"})
	if err != nil {
		t.Fatalf("QuickNote: %v", err)
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
	if !strings.Contains(gotMessage, "速记整理助手") {
		t.Fatalf("提示词应当含速记指令: %q", gotMessage)
	}
	if !strings.HasSuffix(gotMessage, "\n买牛奶") {
		t.Fatalf("正文应当直接追加在提示词末尾: %q", gotMessage)
	}
	if res.Title != "待办" || res.Content != "- 买牛奶" || res.Model != "m1" {
		t.Fatalf("res = %+v", res)
	}
}

func TestQuickNoteStripsFence(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"title\\\":\\\"待办\\\",\\\"content\\\":\\\"- 买牛奶\\\"}\\n```\"}}]}")
	})
	res, err := c.QuickNote(context.Background(), Request{Content: "买牛奶"})
	if err != nil {
		t.Fatalf("QuickNote: %v", err)
	}
	if res.Title != "待办" || res.Content != "- 买牛奶" {
		t.Fatalf("res = %+v", res)
	}
}

func TestQuickNoteBadJSON(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "不是 JSON")
	})
	_, err := c.QuickNote(context.Background(), Request{Content: "买牛奶"})
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
}

func TestQuickNoteInvalidTitleAndContent(t *testing.T) {
	cases := []string{
		`{"choices":[{"message":{"content":"{\"title\":\"\",\"content\":\"x\"}"}}]}`,
		`{"choices":[{"message":{"content":"{\"title\":\"` + strings.Repeat("字", 61) + `\",\"content\":\"x\"}"}}]}`,
		`{"choices":[{"message":{"content":"{\"title\":\"t\",\"content\":\"\"}"}}]}`,
		`{"choices":[{"message":{"content":"{\"title\":\"t\",\"content\":\"` + strings.Repeat("😀", 2001) + `\"}"}}]}`,
	}
	for i, resp := range cases {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, resp)
		})
		_, err := c.QuickNote(context.Background(), Request{Content: "买牛奶"})
		var qe QuickResultError
		if !errors.As(err, &qe) {
			t.Fatalf("case %d: err = %v, want QuickResultError", i, err)
		}
		if qe.Reason == "" {
			t.Fatalf("case %d: Reason 为空", i)
		}
	}
}

func TestQuickNoteReusesErrorTypes(t *testing.T) {
	t.Run("upstream 401", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":{"message":"bad key"}}`)
		})
		_, err := c.QuickNote(context.Background(), Request{Content: "买牛奶"})
		var ue UpstreamError
		if !errors.As(err, &ue) || ue.Status != http.StatusUnauthorized {
			t.Fatalf("err = %v, want UpstreamError 401", err)
		}
	})
	t.Run("未配置", func(t *testing.T) {
		c := &Client{Models: nil}
		_, err := c.QuickNote(context.Background(), Request{Content: "买牛奶"})
		if !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("err = %v, want ErrNotConfigured", err)
		}
	})
	t.Run("响应超体积", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, strings.Repeat("x", maxResponseBytes+1))
		})
		_, err := c.QuickNote(context.Background(), Request{Content: "买牛奶"})
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})
	t.Run("请求未知模型", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"choices":[{"message":{"content":"{\"title\":\"t\",\"content\":\"c\"}"}}]}`)
		})
		_, err := c.QuickNote(context.Background(), Request{Content: "买牛奶", Model: "nope"})
		var me ModelError
		if !errors.As(err, &me) || me.Model != "nope" {
			t.Fatalf("err = %v, want ModelError", err)
		}
	})
}
