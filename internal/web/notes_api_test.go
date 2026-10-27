package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"note/internal/config"
	"note/internal/store"
)

const notesKDFParams = `{"algo":"pbkdf2","prf":"HMAC-SHA256","iter":150000,"keylen":32}`

// csrfTokenFor 走一次 /api/csrf 拿到双提交令牌。
func csrfTokenFor(t *testing.T, handler http.Handler) string {
	t.Helper()

	rec := doRequest(handler, http.MethodGet, "/api/csrf", "127.0.0.1:8080", "")
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode csrf token: %v", err)
	}
	return body.Token
}

// apiRequest 发送带 CSRF 双提交令牌的 JSON 请求；token 为空则故意不带。
func apiRequest(t *testing.T, handler http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-CSRF-Token", token)
		req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeNote(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var note map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &note); err != nil {
		t.Fatalf("decode note: %v (%s)", err, rec.Body.String())
	}
	return note
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) apiErrorBody {
	t.Helper()

	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, rec.Body.String())
	}
	return body
}

// seedStore 打开同一数据库文件的第二个连接，用于预置主密码与分类
// （这两个端点属于 Task 6，笔记接口测试不依赖它们）。
func seedStore(t *testing.T, cfg config.Config) *store.Store {
	t.Helper()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open seed store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close seed store: %v", err)
		}
	})
	return st
}

// kdfParamsJSON 是 kdf_params 在线格式：一个内含 JSON 文本的字符串。
func kdfParamsJSON(t *testing.T, raw string) string {
	t.Helper()

	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal kdf params: %v", err)
	}
	return string(data)
}

func TestNotesAPICreateAndValidation(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	params := kdfParamsJSON(t, notesKDFParams)

	rec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"title":"会议纪要","content":"正文内容"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeNote(t, rec)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("expected id in response: %+v", created)
	}
	if created["encrypted"] != false || created["content"] != "正文内容" {
		t.Fatalf("unexpected note shape: %+v", created)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected no-store on note response, got %q", got)
	}

	cases := []struct {
		name  string
		body  string
		field string
	}{
		{"empty title", `{"title":"  ","content":"x"}`, "title"},
		{"long title", `{"title":"` + strings.Repeat("字", 201) + `","content":"x"}`, "title"},
		{"long content", `{"title":"t","content":"` + strings.Repeat("字", 2001) + `"}`, "content"},
		{"emoji overflow", `{"title":"t","content":"` + strings.Repeat("🚀", 2001) + `"}`, "content"},
		{"no body at all", `{"title":"t"}`, "content"},
		{"bad category", `{"title":"t","content":"c","category_id":4242}`, "category_id"},
		{"cipher without params", `{"title":"t","cipher_b64":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))) + `"}`, "kdf_params"},
		{"cipher not base64", `{"title":"t","cipher_b64":"!!!","kdf_params":` + params + `}`, "cipher_b64"},
		{"cipher too short", `{"title":"t","cipher_b64":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 28))) + `","kdf_params":` + params + `}`, "cipher_b64"},
		{"unknown algo", `{"title":"t","cipher_b64":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))) + `","kdf_params":"{\"algo\":\"scrypt\"}"}`, "kdf_params"},
		{"iter too low", `{"title":"t","cipher_b64":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))) + `","kdf_params":"{\"algo\":\"pbkdf2\",\"iter\":99999,\"keylen\":32}"}`, "kdf_params"},
		{"iter too high", `{"title":"t","cipher_b64":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))) + `","kdf_params":"{\"algo\":\"pbkdf2\",\"iter\":2000000,\"keylen\":32}"}`, "kdf_params"},
		{"keylen wrong", `{"title":"t","cipher_b64":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))) + `","kdf_params":"{\"algo\":\"pbkdf2\",\"iter\":150000,\"keylen\":16}"}`, "kdf_params"},
	}
	for _, tc := range cases {
		rec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", tc.name, rec.Code, rec.Body.String())
		}
		if code := errorBody(t, rec).Error.Code; code != "validation_failed" {
			t.Fatalf("%s: expected validation_failed, got %q", tc.name, code)
		}
		if !strings.Contains(rec.Body.String(), `"`+tc.field+`"`) {
			t.Fatalf("%s: expected field %q in error body: %s", tc.name, tc.field, rec.Body.String())
		}
	}

	// 2000 码点 emoji 正文必须通过：长度按 Unicode 码点计，不是 UTF-16 长度
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"title":"emoji","content":"`+strings.Repeat("🚀", 2000)+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 2000-emoji note accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", "", `{"title":"无令牌","content":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without csrf token, got %d", rec.Code)
	}

	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{broken`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", rec.Code)
	}
	if code := errorBody(t, rec).Error.Code; code != "body_invalid" {
		t.Fatalf("expected body_invalid, got %q", code)
	}
}

func TestNotesAPIEncryptedNote(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	params := kdfParamsJSON(t, notesKDFParams)
	cipherB64 := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	encryptedBody := `{"title":"密","cipher_b64":"` + cipherB64 + `","kdf_params":` + params + `}`

	// 未配置主密码时无法保存密文
	rec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, encryptedBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 without master password, got %d: %s", rec.Code, rec.Body.String())
	}
	if code := errorBody(t, rec).Error.Code; code != "master_password_missing" {
		t.Fatalf("expected master_password_missing, got %q", code)
	}

	st := seedStore(t, cfg)
	if _, err := st.SetMasterPassword(context.Background(), make([]byte, 16), "你的生日是多少？"); err != nil {
		t.Fatalf("seed master password: %v", err)
	}

	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, encryptedBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for encrypted note, got %d: %s", rec.Code, rec.Body.String())
	}
	note := decodeNote(t, rec)
	id, _ := note["id"].(string)
	if note["encrypted"] != true {
		t.Fatalf("expected encrypted true: %+v", note)
	}
	if value, present := note["content"]; present && value != nil {
		t.Fatalf("encrypted note must not carry content: %+v", note)
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/notes/"+id, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := decodeNote(t, rec)["cipher_b64"]; got != cipherB64 {
		t.Fatalf("expected cipher_b64 echo, got %v", got)
	}

	// 密文超过 64 KiB：字段级 400
	hugeCipher := base64.StdEncoding.EncodeToString(make([]byte, 70000))
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token,
		`{"title":"t","cipher_b64":"`+hugeCipher+`","kdf_params":`+params+`}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized cipher, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "cipher_b64") {
		t.Fatalf("expected cipher_b64 field error, got %s", rec.Body.String())
	}

	// 请求体上限：超过 128 KiB 直接 413，不静默截断
	oversized := `{"title":"t","content":"` + strings.Repeat("a", maxNoteBody+1024) + `"}`
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, oversized)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d", rec.Code)
	}
}

func TestNotesAPIClientSuppliedID(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	fixed := store.NewNoteID()
	rec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"id":"`+fixed+`","title":"预分配","content":"x"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := decodeNote(t, rec)["id"]; got != fixed {
		t.Fatalf("expected client id echoed, got %v", got)
	}

	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"id":"not-a-uuid","title":"非法","content":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed id, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"id":"`+fixed+`","title":"重复","content":"x"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate id, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestNotesAPIListPagingAndFilter(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	st := seedStore(t, cfg)
	category, err := st.CreateCategory(context.Background(), "工作")
	if err != nil {
		t.Fatalf("seed category: %v", err)
	}

	for i := 0; i < 25; i++ {
		body := `{"title":"笔记` + string(rune('A'+i)) + `","content":"正文"}`
		if i < 3 {
			body = `{"title":"笔记` + string(rune('A'+i)) + `","content":"正文","category_id":` + strconv.FormatInt(category.ID, 10) + `}`
		}
		rec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create note %d: got %d: %s", i, rec.Code, rec.Body.String())
		}
	}

	rec := apiRequest(t, handler, http.MethodGet, "/api/notes", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items   []map[string]any `json:"items"`
		Total   int              `json:"total"`
		Page    int              `json:"page"`
		PerPage int              `json:"per_page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list page: %v (%s)", err, rec.Body.String())
	}
	if len(page.Items) != 20 || page.Total != 25 || page.Page != 1 || page.PerPage != 20 {
		t.Fatalf("unexpected paging: %d items, total %d, page %d, per_page %d",
			len(page.Items), page.Total, page.Page, page.PerPage)
	}
	if _, present := page.Items[0]["content"]; !present {
		t.Fatalf("expected summary content in list items: %+v", page.Items[0])
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/notes?page=0", token, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page 0: %v", err)
	}
	if page.Page != 1 || len(page.Items) != 20 {
		t.Fatalf("expected page=0 treated as 1, got page %d items %d", page.Page, len(page.Items))
	}

	rec = apiRequest(t, handler, http.MethodGet,
		"/api/notes?category="+strconv.FormatInt(category.ID, 10), token, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode filtered page: %v", err)
	}
	if page.Total != 3 || len(page.Items) != 3 {
		t.Fatalf("expected 3 notes in category, got %d/%d", len(page.Items), page.Total)
	}
	if name, _ := page.Items[0]["category_name"].(string); name != "工作" {
		t.Fatalf("expected category_name 工作, got %q", name)
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/notes?q=%E7%AC%94%E8%AE%B0A", token, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode search page: %v", err)
	}
	if page.Total < 1 || page.Items[0]["title"] != "笔记A" {
		t.Fatalf("expected search hits with title match first, got %+v", page.Items)
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/stats", token, "")
	var stats struct {
		TotalSearches int `json:"total_searches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.TotalSearches < 1 {
		t.Fatalf("expected search activity recorded, got %d", stats.TotalSearches)
	}
}

func TestNotesAPIGetUpdateDelete(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"title":"待办","content":"v1"}`)
	id, _ := decodeNote(t, rec)["id"].(string)

	rec = apiRequest(t, handler, http.MethodGet, "/api/notes/"+id, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := decodeNote(t, rec)["content"]; got != "v1" {
		t.Fatalf("expected content v1, got %v", got)
	}

	rec = apiRequest(t, handler, http.MethodPut, "/api/notes/"+id, token, `{"title":"待办2","content":"v2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d: %s", rec.Code, rec.Body.String())
	}
	if rev, _ := decodeNote(t, rec)["rev"].(float64); rev != 2 {
		t.Fatalf("expected rev 2, got %v", decodeNote(t, rec)["rev"])
	}

	rec = apiRequest(t, handler, http.MethodDelete, "/api/notes/"+id, token, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty body on 204, got %q", rec.Body.String())
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/notes/"+id, token, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
	if code := errorBody(t, rec).Error.Code; code != "not_found" {
		t.Fatalf("expected not_found, got %q", code)
	}

	rec = apiRequest(t, handler, http.MethodPut, "/api/notes/missing-id", token, `{"title":"x","content":"y"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on updating missing note, got %d", rec.Code)
	}
	rec = apiRequest(t, handler, http.MethodDelete, "/api/notes/missing-id", token, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on deleting missing note, got %d", rec.Code)
	}
}
