package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// createPlainNoteViaAPI 走 API 建一条明文未分类笔记，返回其 id。
func createPlainNoteViaAPI(t *testing.T, handler http.Handler, token, title, content string) string {
	t.Helper()

	// json.Marshal 保证多行正文/特殊字符被正确转义，字符串拼接会产出非法 JSON。
	payload, err := json.Marshal(map[string]string{"title": title, "content": content})
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}
	rec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, string(payload))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %q: %d %s", title, rec.Code, rec.Body.String())
	}
	id, _ := decodeNote(t, rec)["id"].(string)
	if id == "" {
		t.Fatalf("create %q: empty id", title)
	}
	return id
}

func TestMergeAPISuccess(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	idA := createPlainNoteViaAPI(t, handler, token, "A", "正文一")
	idB := createPlainNoteViaAPI(t, handler, token, "B", "正文二")

	rec := apiRequest(t, handler, http.MethodPost, "/api/notes/merge", token,
		`{"ids":["`+idA+`","`+idB+`"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected no-store on merge response, got %q", got)
	}
	merged := decodeNote(t, rec)
	const wantContent = "正文一\n\n---\n\n正文二"
	if merged["content"] != wantContent {
		t.Fatalf("content = %v, want %q", merged["content"], wantContent)
	}
	if merged["category_id"] != nil {
		t.Fatalf("merged note should be uncategorized, got %v", merged["category_id"])
	}
	title, _ := merged["title"].(string)
	if !regexp.MustCompile(`^合并-\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`).MatchString(title) {
		t.Fatalf("title = %q, want 合并-<本地时间> 格式", title)
	}

	// 原文一行不删：列表里两条都还在。
	rec = apiRequest(t, handler, http.MethodGet, "/api/notes", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	list := rec.Body.String()
	if !strings.Contains(list, `"title":"A"`) || !strings.Contains(list, `"title":"B"`) {
		t.Fatalf("original notes missing after merge: %s", list)
	}
}

func TestMergeAPIExplicitTitle(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	idA := createPlainNoteViaAPI(t, handler, token, "A", "甲")
	idB := createPlainNoteViaAPI(t, handler, token, "B", "乙")

	rec := apiRequest(t, handler, http.MethodPost, "/api/notes/merge", token,
		`{"ids":["`+idA+`","`+idB+`"],"title":"合并周报"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	merged := decodeNote(t, rec)
	if merged["title"] != "合并周报" {
		t.Fatalf("title = %v, want 合并周报", merged["title"])
	}
	if merged["category_id"] != nil {
		t.Fatalf("merged note should be uncategorized, got %v", merged["category_id"])
	}
	if merged["content"] != "甲\n\n---\n\n乙" {
		t.Fatalf("content = %v, want %q", merged["content"], "甲\n\n---\n\n乙")
	}
}

func TestMergeAPICSRFRequired(t *testing.T) {
	handler, _ := newTestHandler(t)
	// 不带 CSRF 令牌：即使 ids 合法也必须 403。
	rec := apiRequest(t, handler, http.MethodPost, "/api/notes/merge", "",
		`{"ids":["00000000-0000-4000-8000-000000000000","00000000-0000-4000-8000-000000000001"]}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMergeAPIValidation(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	idA := createPlainNoteViaAPI(t, handler, token, "A", "甲")
	idB := createPlainNoteViaAPI(t, handler, token, "B", "乙")

	cases := []struct {
		name  string
		body  string
		field string
	}{
		{"少于两条", `{"ids":["` + idA + `"]}`, "ids"},
		{"非法 UUID", `{"ids":["not-a-uuid","` + idB + `"]}`, "ids"},
		{"重复 id", `{"ids":["` + idA + `","` + idA + `"]}`, "ids"},
		{"标题超长", `{"ids":["` + idA + `","` + idB + `"],"title":"` + strings.Repeat("字", 201) + `"}`, "title"},
	}
	for _, tc := range cases {
		rec := apiRequest(t, handler, http.MethodPost, "/api/notes/merge", token, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", tc.name, rec.Code, rec.Body.String())
		}
		fields := errorBody(t, rec).Error.Fields
		if _, ok := fields[tc.field]; !ok {
			t.Fatalf("%s: expected field %q in error, got %+v", tc.name, tc.field, fields)
		}
	}
}

func TestMergeAPINotFound(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	idA := createPlainNoteViaAPI(t, handler, token, "A", "甲")
	ghost := "00000000-0000-4000-8000-000000000000"

	rec := apiRequest(t, handler, http.MethodPost, "/api/notes/merge", token,
		`{"ids":["`+idA+`","`+ghost+`"]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	if code := errorBody(t, rec).Error.Code; code != "not_found" {
		t.Fatalf("code = %q, want not_found", code)
	}
}

func TestMergeAPITrash(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	idA := createPlainNoteViaAPI(t, handler, token, "A", "甲")
	idB := createPlainNoteViaAPI(t, handler, token, "B", "乙")

	if rec := apiRequest(t, handler, http.MethodDelete, "/api/notes/"+idA, token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("soft delete A: %d %s", rec.Code, rec.Body.String())
	}

	rec := apiRequest(t, handler, http.MethodPost, "/api/notes/merge", token,
		`{"ids":["`+idA+`","`+idB+`"]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if code := errorBody(t, rec).Error.Code; code != "note_in_trash" {
		t.Fatalf("code = %q, want note_in_trash", code)
	}
}

func TestMergeAPIEncryptedInSelection(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	idA := createPlainNoteViaAPI(t, handler, token, "A", "甲")

	// 预置主密码后建一条加密笔记。
	st := seedStore(t, cfg)
	if _, err := st.SetMasterPassword(context.Background(), make([]byte, 16), "你的生日是多少？"); err != nil {
		t.Fatalf("seed master password: %v", err)
	}
	params := kdfParamsJSON(t, notesKDFParams)
	cipherB64 := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	encBody := `{"title":"密","cipher_b64":"` + cipherB64 + `","kdf_params":` + params + `}`
	rec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, encBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create encrypted note: %d %s", rec.Code, rec.Body.String())
	}
	idEnc, _ := decodeNote(t, rec)["id"].(string)

	rec = apiRequest(t, handler, http.MethodPost, "/api/notes/merge", token,
		`{"ids":["`+idA+`","`+idEnc+`"]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if code := errorBody(t, rec).Error.Code; code != "encrypted_in_selection" {
		t.Fatalf("code = %q, want encrypted_in_selection", code)
	}
}

func TestMergeAPITitleConflict(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	// 未分类里已有一条「合并周报」。
	createPlainNoteViaAPI(t, handler, token, "合并周报", "旧")
	idA := createPlainNoteViaAPI(t, handler, token, "A", "甲")
	idB := createPlainNoteViaAPI(t, handler, token, "B", "乙")

	rec := apiRequest(t, handler, http.MethodPost, "/api/notes/merge", token,
		`{"ids":["`+idA+`","`+idB+`"],"title":"合并周报"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if code := errorBody(t, rec).Error.Code; code != "title_conflict" {
		t.Fatalf("code = %q, want title_conflict", code)
	}
}

func TestMergeAPIContentTooLong(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	long := strings.Repeat("字", 1100)
	idA := createPlainNoteViaAPI(t, handler, token, "A", long)
	idB := createPlainNoteViaAPI(t, handler, token, "B", long)

	rec := apiRequest(t, handler, http.MethodPost, "/api/notes/merge", token,
		`{"ids":["`+idA+`","`+idB+`"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	fields := errorBody(t, rec).Error.Fields
	if _, ok := fields["content"]; !ok {
		t.Fatalf("expected content field error, got %+v", fields)
	}
}
