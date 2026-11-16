package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestQuickNoteCreatesWithAutoTitle(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/quick", token, `{"content":"买牛奶"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/quick = %d: %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("含正文的响应必须 no-store")
	}
	note := decodeNote(t, rec)
	if !regexp.MustCompile(`^速记-\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`).MatchString(note["title"].(string)) {
		t.Fatalf("标题格式不符: %v", note["title"])
	}
	if note["category_id"] != float64(2) {
		t.Fatalf("category_id = %v", note["category_id"])
	}
	if note["content"] != "买牛奶" {
		t.Fatalf("content = %v", note["content"])
	}

	// 已落库：能按 id 取回。
	id := note["id"].(string)
	got := apiRequest(t, handler, http.MethodGet, "/api/notes/"+id, token, "")
	if got.Code != http.StatusOK {
		t.Fatalf("GET /api/notes/%s = %d: %s", id, got.Code, got.Body)
	}
}

func TestQuickNoteWithExplicitTitle(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/quick", token, `{"content":"买牛奶","title":"今日待办"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body)
	}
	note := decodeNote(t, rec)
	if note["title"] != "今日待办" {
		t.Fatalf("title = %v", note["title"])
	}
}

func TestQuickNoteValidation(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	for _, tc := range []struct {
		name, body, field string
	}{
		{"正文为空", `{"content":"  "}`, "content"},
		{"正文超长", `{"content":"` + strings.Repeat("😀", 2001) + `"}`, "content"},
		{"标题超长", `{"content":"x","title":"` + strings.Repeat("字", 201) + `"}`, "title"},
	} {
		rec := apiRequest(t, handler, http.MethodPost, "/api/quick", token, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: POST = %d: %s", tc.name, rec.Code, rec.Body)
		}
		eb := errorBody(t, rec)
		if eb.Error.Code != "validation_failed" {
			t.Fatalf("%s: code = %q", tc.name, eb.Error.Code)
		}
		if _, ok := eb.Error.Fields[tc.field]; !ok {
			t.Fatalf("%s: fields 缺 %s: %+v", tc.name, tc.field, eb.Error.Fields)
		}
	}
}

func TestQuickNoteRequiresCSRF(t *testing.T) {
	handler, _ := newTestHandler(t)
	rec := apiRequest(t, handler, http.MethodPost, "/api/quick", "", `{"content":"买牛奶"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without CSRF = %d: %s", rec.Code, rec.Body)
	}
}

func TestQuickNoteTitleConflict(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	if rec := apiRequest(t, handler, http.MethodPost, "/api/quick", token, `{"content":"x","title":"我写过的"}`); rec.Code != http.StatusCreated {
		t.Fatalf("第一次创建 = %d: %s", rec.Code, rec.Body)
	}
	rec := apiRequest(t, handler, http.MethodPost, "/api/quick", token, `{"content":"y","title":"我写过的"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("第二次 = %d: %s", rec.Code, rec.Body)
	}
	eb := errorBody(t, rec)
	if eb.Error.Code != "title_conflict" {
		t.Fatalf("code = %q", eb.Error.Code)
	}
}

func TestAIQuickNoteUnconfigured(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/quick", token, `{"content":"买牛奶"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body)
	}
	eb := errorBody(t, rec)
	if eb.Error.Code != "ai_not_configured" {
		t.Fatalf("code = %q", eb.Error.Code)
	}
	if !strings.Contains(eb.Error.Message, "ai.base_url") {
		t.Fatalf("消息应提示缺哪几项: %q", eb.Error.Message)
	}
}

func TestAIQuickNoteHappyPath(t *testing.T) {
	handler, cfg := newTestHandler(t)
	upstream := fakeUpstream(t, `{"title":"待办","content":"- 买牛奶"}`)
	configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n")
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/quick", token, `{"content":"买牛奶"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("含正文的响应必须 no-store")
	}
	var body struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Model   string `json:"model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body)
	}
	if body.Title != "待办" || body.Content != "- 买牛奶" || body.Model != "m1" {
		t.Fatalf("body = %+v", body)
	}
}

func TestAIQuickNoteDoesNotPersist(t *testing.T) {
	handler, cfg := newTestHandler(t)
	upstream := fakeUpstream(t, `{"title":"待办","content":"- 买牛奶"}`)
	configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n")
	token := csrfTokenFor(t, handler)

	if rec := apiRequest(t, handler, http.MethodPost, "/api/ai/quick", token, `{"content":"买牛奶"}`); rec.Code != http.StatusOK {
		t.Fatalf("AI 预览 = %d: %s", rec.Code, rec.Body)
	}
	rec := apiRequest(t, handler, http.MethodGet, "/api/notes", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/notes = %d", rec.Code)
	}
	var page struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("AI 预览不应落库，total = %d", page.Total)
	}
}

func TestAIQuickNoteBadResultReason(t *testing.T) {
	handler, cfg := newTestHandler(t)
	upstream := fakeUpstream(t, `{"title":"","content":"x"}`)
	configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n")
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/ai/quick", token, `{"content":"买牛奶"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body)
	}
	eb := errorBody(t, rec)
	if eb.Error.Code != "ai_bad_response" {
		t.Fatalf("code = %q", eb.Error.Code)
	}
	if !strings.Contains(eb.Error.Message, "标题是空的") {
		t.Fatalf("消息应带明确原因: %q", eb.Error.Message)
	}
}

func TestAIQuickNoteValidation(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	for _, body := range []string{`{"content":"  "}`, `{"content":"` + strings.Repeat("😀", 2001) + `"}`} {
		rec := apiRequest(t, handler, http.MethodPost, "/api/ai/quick", token, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST = %d: %s", rec.Code, rec.Body)
		}
	}
}
