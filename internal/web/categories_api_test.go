package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func categoryPath(id int64) string {
	return "/api/categories/" + strconv.FormatInt(id, 10)
}

func TestMasterPasswordAPI(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodGet, "/api/master-password", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var state struct {
		Configured bool   `json:"configured"`
		Question   string `json:"question"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.Configured || state.Question != "" {
		t.Fatalf("expected unconfigured state, got %+v", state)
	}

	saltB64 := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 16)))
	body := `{"salt_b64":"` + saltB64 + `","question":"你的生日是多少？"}`

	rec = apiRequest(t, handler, http.MethodPost, "/api/master-password", "", body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without csrf token, got %d", rec.Code)
	}

	rec = apiRequest(t, handler, http.MethodPost, "/api/master-password", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, handler, http.MethodPost, "/api/master-password", token, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on second set, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/master-password", token, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode state after set: %v", err)
	}
	if !state.Configured || state.Question != "你的生日是多少？" {
		t.Fatalf("unexpected state: %+v", state)
	}

	// 非法 salt 与空问题都要 400 validation_failed
	for _, payload := range []string{
		`{"salt_b64":"!!!","question":"问题"}`,
		`{"salt_b64":"` + base64.StdEncoding.EncodeToString([]byte("short")) + `","question":"问题"}`,
		`{"salt_b64":"` + saltB64 + `","question":"  "}`,
	} {
		rec := apiRequest(t, handler, http.MethodPost, "/api/master-password", token, payload)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d: %s", payload, rec.Code, rec.Body.String())
		}
		if code := errorBody(t, rec).Error.Code; code != "validation_failed" {
			t.Fatalf("expected validation_failed, got %q", code)
		}
	}
}

func TestCategoriesAPI(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	listCategories := func() []map[string]any {
		t.Helper()
		rec := apiRequest(t, handler, http.MethodGet, "/api/categories", token, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("list categories: %d %s", rec.Code, rec.Body.String())
		}
		var items []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode categories: %v", err)
		}
		return items
	}

	items := listCategories()
	if len(items) != 2 {
		t.Fatalf("expected 未分类 + 速记, got %+v", items)
	}
	defaultID, _ := items[0]["id"].(float64)
	if name, _ := items[0]["name"].(string); name != "未分类" {
		t.Fatalf("expected 未分类 first, got %q", name)
	}
	// 内置固定分类都带 reserved 标记：前端靠它决定不给改名/删除按钮。
	for _, item := range items {
		if reserved, _ := item["reserved"].(bool); !reserved {
			t.Fatalf("expected built-in category to be reserved, got %+v", item)
		}
	}
	if name, _ := items[1]["name"].(string); name != "速记" {
		t.Fatalf("expected 速记 second, got %q", name)
	}

	rec := apiRequest(t, handler, http.MethodPost, "/api/categories", token, `{"name":"工作"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created category: %v", err)
	}
	if created.Name != "工作" || created.ID <= int64(defaultID) {
		t.Fatalf("unexpected created category: %+v", created)
	}

	// 撞名（NOCASE，只差大小写也算）是冲突而非输入错误：409 + code + fields.name，
	// 与 id_conflict / category_reserved / category_not_empty 同族。
	rec = apiRequest(t, handler, http.MethodPost, "/api/categories", token, `{"name":"工作"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate name, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := errorBody(t, rec); body.Error.Code != "category_exists" || body.Error.Fields["name"] == "" {
		t.Fatalf("expected category_exists with name field, got %+v", body.Error)
	}

	for _, payload := range []string{`{"name":""}`, `{"name":"   "}`, `{"name":"` + strings.Repeat("名", 31) + `"}`} {
		rec := apiRequest(t, handler, http.MethodPost, "/api/categories", token, payload)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d", payload, rec.Code)
		}
	}

	rec = apiRequest(t, handler, http.MethodPut, categoryPath(created.ID), token, `{"name":"职业"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on rename, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, handler, http.MethodPut, categoryPath(int64(defaultID)), token, `{"name":"杂事"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 renaming default, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = apiRequest(t, handler, http.MethodDelete, categoryPath(int64(defaultID)), token, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 deleting default, got %d: %s", rec.Code, rec.Body.String())
	}

	// 分类下有笔记时删除被拒，响应需带笔记数
	rec = apiRequest(t, handler, http.MethodPost, "/api/notes", token,
		`{"title":"周报","content":"正文","category_id":`+strconv.FormatInt(created.ID, 10)+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 creating categorized note, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, handler, http.MethodDelete, categoryPath(created.ID), token, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 deleting in-use category, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "1") {
		t.Fatalf("expected note count in error body, got %s", rec.Body.String())
	}

	items = listCategories()
	for _, item := range items {
		if id, _ := item["id"].(float64); int64(id) == created.ID {
			if count, _ := item["note_count"].(float64); count != 1 {
				t.Fatalf("expected note_count 1, got %v", item)
			}
		}
	}

	// 删掉笔记后即可删除分类
	rec = apiRequest(t, handler, http.MethodGet, "/api/notes", token, "")
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode notes page: %v", err)
	}
	rec = apiRequest(t, handler, http.MethodDelete, "/api/notes/"+page.Items[0].ID, token, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 deleting note, got %d", rec.Code)
	}

	rec = apiRequest(t, handler, http.MethodDelete, categoryPath(created.ID), token, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 deleting empty category, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(listCategories()) != 2 {
		t.Fatalf("expected only the two built-in categories left, got %+v", listCategories())
	}

	rec = apiRequest(t, handler, http.MethodPut, "/api/categories/9999", token, `{"name":"幽灵"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 renaming missing category, got %d", rec.Code)
	}
	rec = apiRequest(t, handler, http.MethodPost, "/api/categories", "", `{"name":"无令牌"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without csrf token, got %d", rec.Code)
	}
}

func TestMasterPasswordStoreSeeded(t *testing.T) {
	// 守护 seedStore 的用法：主密码写入后 POST 必须 409；GET 回显公开盐供浏览器派生密钥
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	st := seedStore(t, cfg)
	salt := []byte("0123456789abcdef")
	if len(salt) != 16 {
		t.Fatalf("test salt must be 16 bytes, got %d", len(salt))
	}
	if _, err := st.SetMasterPassword(context.Background(), salt, "问题"); err != nil {
		t.Fatalf("seed master password: %v", err)
	}

	rec := apiRequest(t, handler, http.MethodPost, "/api/master-password", token,
		`{"salt_b64":"`+base64.StdEncoding.EncodeToString(make([]byte, 16))+`","question":"另一个"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/master-password", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var master struct {
		Configured bool   `json:"configured"`
		SaltB64    string `json:"salt_b64"`
		Question   string `json:"question"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &master); err != nil {
		t.Fatalf("decode master: %v", err)
	}
	if !master.Configured || master.Question != "问题" {
		t.Fatalf("unexpected master payload: %+v", master)
	}
	if master.SaltB64 != base64.StdEncoding.EncodeToString(salt) {
		t.Fatalf("GET must echo the public salt for key derivation, got %q", master.SaltB64)
	}
	if strings.Contains(rec.Body.String(), "\"password\"") {
		t.Fatalf("GET must not carry any password field, got %s", rec.Body.String())
	}
}

// 撞名（含只差大小写）是状态冲突而不是输入格式错误：与 category_not_empty、
// id_conflict 同族走 409，同时保留 fields.name 让前端把提示标在输入框上。
func TestCategoriesAPIDuplicateNameIsConflict(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	create := func(name string) int64 {
		t.Helper()
		rec := apiRequest(t, handler, http.MethodPost, "/api/categories", token, `{"name":"`+name+`"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %q: got %d: %s", name, rec.Code, rec.Body.String())
		}
		var cat struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &cat); err != nil {
			t.Fatalf("decode %q: %v", name, err)
		}
		return cat.ID
	}

	work := create("Work")
	temp := create("Temp")

	// 改名撞进别人的 NOCASE 唯一键：不能让 SQLite 约束错误冒成 500
	rec := apiRequest(t, handler, http.MethodPut, categoryPath(temp), token, `{"name":"work"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 renaming onto a case-variant name, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := errorBody(t, rec); body.Error.Code != "category_exists" || body.Error.Fields["name"] == "" {
		t.Fatalf("expected category_exists with name field on rename, got %+v", body.Error)
	}

	rec = apiRequest(t, handler, http.MethodPut, categoryPath(work), token, `{"name":"未分类"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 renaming onto the default name, got %d: %s", rec.Code, rec.Body.String())
	}
}
