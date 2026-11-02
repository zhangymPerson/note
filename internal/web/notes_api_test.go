package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestStatsCountsCRUDAndViewEvents(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"title":"账簿","content":"正文"}`)
	id, _ := decodeNote(t, rec)["id"].(string)

	if rec = apiRequest(t, handler, http.MethodGet, "/api/notes/"+id, token, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on detail, got %d", rec.Code)
	}
	if rec = apiRequest(t, handler, http.MethodPut, "/api/notes/"+id, token, `{"title":"账簿2","content":"正文"}`); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d", rec.Code)
	}

	rec = apiRequest(t, handler, http.MethodPost, "/api/categories", token, `{"name":"工作"}`)
	var category struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &category); err != nil {
		t.Fatalf("decode category: %v (%s)", err, rec.Body.String())
	}
	catPath := "/api/categories/" + strconv.FormatInt(category.ID, 10)
	if rec = apiRequest(t, handler, http.MethodPut, catPath, token, `{"name":"工作台"}`); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on rename, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec = apiRequest(t, handler, http.MethodDelete, catPath, token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on category delete, got %d: %s", rec.Code, rec.Body.String())
	}

	if rec = apiRequest(t, handler, http.MethodDelete, "/api/notes/"+id, token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete, got %d: %s", rec.Code, rec.Body.String())
	}
	// 回收站里的详情读取不算「查看」：删掉的笔记看的是维护动作，不是使用
	if rec = apiRequest(t, handler, http.MethodGet, "/api/notes/"+id, token, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on trashed detail, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "deleted_at") {
		t.Fatalf("expected deleted_at in trashed detail: %s", body)
	}
	if rec = apiRequest(t, handler, http.MethodGet, "/api/notes?q=%E8%B4%A6%E7%B0%BF", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on search, got %d", rec.Code)
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/stats", token, "")
	var stats struct {
		TotalEvents   int `json:"total_events"`
		TotalSearches int `json:"total_searches"`
		ActiveDays    int `json:"active_days"`
		EventsByKind  []struct {
			Kind  string `json:"kind"`
			Count int    `json:"count"`
		} `json:"events_by_kind"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v (%s)", err, rec.Body.String())
	}

	want := map[string]int{
		store.ActivityNoteCreate:     1,
		store.ActivityNoteView:       1,
		store.ActivityNoteUpdate:     1,
		store.ActivityNoteDelete:     1,
		store.ActivityCategoryCreate: 1,
		store.ActivityCategoryRename: 1,
		store.ActivityCategoryDelete: 1,
		store.ActivitySearch:         1,
	}
	got := map[string]int{}
	for _, item := range stats.EventsByKind {
		got[item.Kind] = item.Count
	}
	for kind, count := range want {
		if got[kind] != count {
			t.Fatalf("expected %d %s events, got %d (%+v)", count, kind, got[kind], stats.EventsByKind)
		}
	}
	if stats.TotalEvents != 8 {
		t.Fatalf("expected 8 total events, got %d", stats.TotalEvents)
	}
	if stats.TotalSearches != 1 {
		t.Fatalf("expected 1 search, got %d", stats.TotalSearches)
	}
	if stats.ActiveDays != 1 {
		t.Fatalf("expected 1 active day, got %d", stats.ActiveDays)
	}
}

func TestNotesAPISearchQueryLengthLimit(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodGet,
		"/api/notes?q="+url.QueryEscape(strings.Repeat("查", 201)), token, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized query, got %d: %s", rec.Code, rec.Body.String())
	}
	body := errorBody(t, rec)
	if body.Error.Fields["q"] == "" {
		t.Fatalf("expected field error for q, got %+v", body.Error)
	}

	rec = apiRequest(t, handler, http.MethodGet,
		"/api/notes?q="+url.QueryEscape(strings.Repeat("查", 200)), token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a 200-rune query, got %d: %s", rec.Code, rec.Body.String())
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

	// 删除是软删除：详情仍可读，但带上 deleted_at，且不在默认列表里
	rec = apiRequest(t, handler, http.MethodGet, "/api/notes/"+id, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on trashed detail, got %d", rec.Code)
	}
	if deletedAt, _ := decodeNote(t, rec)["deleted_at"].(string); deletedAt == "" {
		t.Fatalf("expected deleted_at on trashed note, got %v", decodeNote(t, rec))
	}
	rec = apiRequest(t, handler, http.MethodGet, "/api/notes", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on list, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "待办2") {
		t.Fatalf("trashed note must not show up in the default list: %s", rec.Body.String())
	}

	// 彻底删除只对回收站里的笔记生效，删完就真的不存在了
	rec = apiRequest(t, handler, http.MethodDelete, "/api/notes/"+id+"?purge=1", token, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on purge, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, handler, http.MethodGet, "/api/notes/"+id, token, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after purge, got %d", rec.Code)
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

func TestNotesAPIRecycleBinFlow(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/categories", token, `{"name":"归档"}`)
	var category struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &category); err != nil {
		t.Fatalf("decode category: %v (%s)", err, rec.Body.String())
	}
	payload := `{"title":"待归档","content":"正文","category_id":` + strconv.FormatInt(category.ID, 10) + `}`
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, payload)
	id, _ := decodeNote(t, rec)["id"].(string)

	// 删除 = 移入回收站
	if rec = apiRequest(t, handler, http.MethodDelete, "/api/notes/"+id, token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on soft delete, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, handler, http.MethodGet, "/api/notes?deleted=1", token, "")
	var page struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode trash page: %v (%s)", err, rec.Body.String())
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0]["id"] != id {
		t.Fatalf("expected the note in the recycle bin, got %+v", page)
	}
	if deletedAt, _ := page.Items[0]["deleted_at"].(string); deletedAt == "" {
		t.Fatalf("expected deleted_at in the recycle bin row, got %+v", page.Items[0])
	}

	// 回收站列表不影响默认列表，也不受分类筛选牵连
	rec = apiRequest(t, handler, http.MethodGet, "/api/notes", token, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode default page: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("expected empty default list, got %+v", page)
	}

	// 已经在回收站里：再删一次 404，彻底删除之后才真消失
	if rec = apiRequest(t, handler, http.MethodDelete, "/api/notes/"+id, token, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on repeated delete, got %d", rec.Code)
	}

	rec = apiRequest(t, handler, http.MethodPost, "/api/notes/"+id+"/restore", token, "null")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on restore, got %d: %s", rec.Code, rec.Body.String())
	}
	restored := decodeNote(t, rec)
	if _, ok := restored["deleted_at"]; ok {
		t.Fatalf("expected deleted_at cleared after restore, got %v", restored)
	}
	if got, _ := restored["category_id"].(float64); int64(got) != category.ID {
		t.Fatalf("expected category %d to survive restore, got %v", category.ID, restored["category_id"])
	}

	// 不在回收站里的笔记：恢复与彻底删除都返回 409，而不是假装成功
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes/"+id+"/restore", token, "null")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on restoring a live note, got %d: %s", rec.Code, rec.Body.String())
	}
	if code := errorBody(t, rec).Error.Code; code != "not_in_trash" {
		t.Fatalf("expected not_in_trash, got %q", code)
	}
	rec = apiRequest(t, handler, http.MethodDelete, "/api/notes/"+id+"?purge=1", token, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on purging a live note, got %d: %s", rec.Code, rec.Body.String())
	}

	// 彻底删除：先移入回收站，再 purge
	if rec = apiRequest(t, handler, http.MethodDelete, "/api/notes/"+id, token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on soft delete, got %d", rec.Code)
	}
	if rec = apiRequest(t, handler, http.MethodDelete, "/api/notes/"+id+"?purge=1", token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on purge, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, handler, http.MethodGet, "/api/notes?deleted=1", token, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode trash page after purge: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("expected empty recycle bin after purge, got %+v", page)
	}

	// 回收站接口同样受 CSRF 双提交保护
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes/"+id+"/restore", "", "null")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without csrf token, got %d", rec.Code)
	}
}

func TestNotesAPITitleConflict(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"title":"周报","content":"第一版"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	firstID, _ := decodeNote(t, rec)["id"].(string)

	// 同一个分类里同名（大小写不敏感）→ 409 title_conflict，不新建第二条。
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"title":"周报","content":"第二版"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate title, got %d: %s", rec.Code, rec.Body.String())
	}
	body := errorBody(t, rec)
	if body.Error.Code != "title_conflict" {
		t.Fatalf("expected title_conflict, got %q", body.Error.Code)
	}
	if body.Error.Fields["title"] == "" {
		t.Fatalf("expected a title field hint: %+v", body.Error)
	}

	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"title":"todo","content":"x"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"title":"TODO","content":"y"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected NOCASE 409, got %d: %s", rec.Code, rec.Body.String())
	}

	// 换一个分类同名可以：标题唯一是「分类内」的唯一。
	rec = apiRequest(t, handler, http.MethodPost, "/api/categories", token, `{"name":"工作"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create category: %d %s", rec.Code, rec.Body.String())
	}
	var work struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &work); err != nil {
		t.Fatalf("decode category: %v", err)
	}
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token,
		`{"title":"周报","content":"另一份","category_id":`+strconv.FormatInt(work.ID, 10)+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 in another category, got %d: %s", rec.Code, rec.Body.String())
	}

	// 改标题撞上邻居也是 409 title_conflict。
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"title":"月报","content":"x"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	monthID, _ := decodeNote(t, rec)["id"].(string)
	rec = apiRequest(t, handler, http.MethodPut, "/api/notes/"+monthID, token, `{"title":"周报","content":"改个名"}`)
	if rec.Code != http.StatusConflict || errorBody(t, rec).Error.Code != "title_conflict" {
		t.Fatalf("expected 409 title_conflict on rename, got %d: %s", rec.Code, rec.Body.String())
	}

	// 软删除后同名新建 → 409 title_in_trash：回收站里的那条仍占着这个名字。
	rec = apiRequest(t, handler, http.MethodDelete, "/api/notes/"+firstID, token, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on soft delete, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token, `{"title":"周报","content":"第三版"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 after delete, got %d: %s", rec.Code, rec.Body.String())
	}
	if code := errorBody(t, rec).Error.Code; code != "title_in_trash" {
		t.Fatalf("expected title_in_trash, got %q", code)
	}
}
