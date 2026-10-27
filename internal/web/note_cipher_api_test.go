package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"note/internal/config"
)

const cipherTestKDF = `{"algo":"pbkdf2","prf":"HMAC-SHA256","iter":150000,"keylen":32}`

// seedMasterAndEncryptedNote 配置主密码并建一条加密笔记，返回其 id 与创建响应。
func seedMasterAndEncryptedNote(t *testing.T, handler http.Handler, cfg config.Config, title string) (string, map[string]any) {
	t.Helper()

	if _, err := seedStore(t, cfg).SetMasterPassword(context.Background(), make([]byte, 16), "你的生日是多少？"); err != nil {
		t.Fatalf("set master password: %v", err)
	}

	token := csrfTokenFor(t, handler)
	body, err := json.Marshal(map[string]any{
		"title":      title,
		"cipher_b64": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))),
		"kdf_params": cipherTestKDF,
	})
	if err != nil {
		t.Fatalf("marshal note body: %v", err)
	}
	rec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	note := decodeNote(t, rec)
	id, _ := note["id"].(string)
	if id == "" {
		t.Fatalf("expected id in response: %+v", note)
	}
	return id, note
}

func TestListEncryptedCiphersRoute(t *testing.T) {
	handler, cfg := newTestHandler(t)
	id, _ := seedMasterAndEncryptedNote(t, handler, cfg, "加密一条")

	body, err := json.Marshal(map[string]any{"title": "明文一条", "content": "正文"})
	if err != nil {
		t.Fatalf("marshal plain note: %v", err)
	}
	plainToken := csrfTokenFor(t, handler)
	if rec := apiRequest(t, handler, http.MethodPost, "/api/notes", plainToken, string(body)); rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for plaintext note, got %d: %s", rec.Code, rec.Body.String())
	}

	// 若 GET /api/notes/{id} 通配吃掉了这条路由，这里会变成 404 not_found 而非 200 清单。
	rec := apiRequest(t, handler, http.MethodGet, "/api/notes/encrypted", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var payload struct {
		Notes []map[string]any `json:"notes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list payload: %v (%s)", err, rec.Body.String())
	}
	if len(payload.Notes) != 1 {
		t.Fatalf("expected only the encrypted note, got %+v", payload.Notes)
	}
	item := payload.Notes[0]
	if item["id"] != id {
		t.Errorf("id = %v, want %q", item["id"], id)
	}
	if cipher, _ := item["cipher_b64"].(string); cipher == "" {
		t.Errorf("cipher_b64 missing in %+v", item)
	}
	if params, _ := item["kdf_params"].(string); params != cipherTestKDF {
		t.Errorf("kdf_params = %q, want %q", params, cipherTestKDF)
	}
	for _, forbidden := range []string{"title", "content"} {
		if _, ok := item[forbidden]; ok {
			t.Errorf("清单不得含 %q：%+v", forbidden, item)
		}
	}
}

func TestListEncryptedCiphersEmitsEmptyArray(t *testing.T) {
	handler, _ := newTestHandler(t)

	rec := apiRequest(t, handler, http.MethodGet, "/api/notes/encrypted", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"notes":[]}` {
		t.Errorf("空清单必须输出 []，不是 null，got %q", got)
	}
}

func TestUpdateNoteCipherRoute(t *testing.T) {
	handler, cfg := newTestHandler(t)
	id, created := seedMasterAndEncryptedNote(t, handler, cfg, "待迁移")
	originalUpdatedAt, _ := created["updated_at"].(string)
	token := csrfTokenFor(t, handler)

	newRaw := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("b", 40)))
	newParams := `{"algo":"pbkdf2","prf":"HMAC-SHA256","iter":200000,"keylen":32}`
	put := func(target string, cipherB64, params string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{"cipher_b64": cipherB64, "kdf_params": params})
		if err != nil {
			t.Fatalf("marshal cipher body: %v", err)
		}
		return apiRequest(t, handler, http.MethodPut, target, token, string(body))
	}

	rec := put("/api/notes/"+id+"/cipher", newRaw, newParams)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated := decodeNote(t, rec)
	if updated["cipher_b64"] != newRaw {
		t.Errorf("cipher_b64 = %v, want the new cipher", updated["cipher_b64"])
	}
	if updated["encrypted"] != true {
		t.Errorf("encrypted = %v, want true", updated["encrypted"])
	}
	// 改密不是用户可见的修订：rev 与 updated_at 必须原样
	if updated["rev"] != created["rev"] {
		t.Errorf("rev = %v, want it unchanged at %v", updated["rev"], created["rev"])
	}
	if updated["updated_at"] != originalUpdatedAt {
		t.Errorf("updated_at = %v, want it unchanged at %q", updated["updated_at"], originalUpdatedAt)
	}

	missingParams := put("/api/notes/"+id+"/cipher", newRaw, "")
	if missingParams.Code != http.StatusBadRequest {
		t.Fatalf("缺 kdf_params: expected 400, got %d: %s", missingParams.Code, missingParams.Body.String())
	}
	if field := errorBody(t, missingParams).Error.Fields["kdf_params"]; field == "" {
		t.Errorf("expected fields.kdf_params message, got %+v", errorBody(t, missingParams).Error)
	}

	badBase64 := put("/api/notes/"+id+"/cipher", "not base64!", cipherTestKDF)
	if badBase64.Code != http.StatusBadRequest {
		t.Fatalf("非法 base64: expected 400, got %d: %s", badBase64.Code, badBase64.Body.String())
	}
	if field := errorBody(t, badBase64).Error.Fields["cipher_b64"]; field == "" {
		t.Errorf("expected fields.cipher_b64 message, got %+v", errorBody(t, badBase64).Error)
	}

	plainBody, err := json.Marshal(map[string]any{"title": "明文", "content": "正文"})
	if err != nil {
		t.Fatalf("marshal plain note: %v", err)
	}
	plainRec := apiRequest(t, handler, http.MethodPost, "/api/notes", token, string(plainBody))
	if plainRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", plainRec.Code, plainRec.Body.String())
	}
	plainID := decodeNote(t, plainRec)["id"].(string)
	conflict := put("/api/notes/"+plainID+"/cipher", newRaw, newParams)
	if code := errorBody(t, conflict); conflict.Code != http.StatusConflict || code.Error.Code != "not_encrypted" {
		t.Errorf("明文笔记: expected 409 not_encrypted, got %d %q", conflict.Code, conflict.Body.String())
	}

	ghost := put("/api/notes/11111111-1111-4111-8111-111111111111/cipher", newRaw, newParams)
	if code := errorBody(t, ghost); ghost.Code != http.StatusNotFound || code.Error.Code != "not_found" {
		t.Errorf("不存在的 id: expected 404 not_found, got %d %q", ghost.Code, ghost.Body.String())
	}

	// 上面的失败请求不得改动已迁移的密文
	final := apiRequest(t, handler, http.MethodGet, "/api/notes/"+id, "", "")
	if decodeNote(t, final)["cipher_b64"] != newRaw {
		t.Errorf("失败请求污染了密文：%s", final.Body.String())
	}
}

func TestCipherRoutesRequireCSRF(t *testing.T) {
	handler, cfg := newTestHandler(t)
	id, created := seedMasterAndEncryptedNote(t, handler, cfg, " CSRF 校验")
	originalCipher, _ := created["cipher_b64"].(string)

	body, err := json.Marshal(map[string]any{
		"cipher_b64": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("c", 40))),
		"kdf_params": cipherTestKDF,
	})
	if err != nil {
		t.Fatalf("marshal cipher body: %v", err)
	}
	rec := apiRequest(t, handler, http.MethodPut, "/api/notes/"+id+"/cipher", "", string(body))
	if code := errorBody(t, rec); rec.Code != http.StatusForbidden || code.Error.Code != "csrf_invalid" {
		t.Fatalf("expected 403 csrf_invalid, got %d %q", rec.Code, rec.Body.String())
	}

	after := apiRequest(t, handler, http.MethodGet, "/api/notes/"+id, "", "")
	if got := decodeNote(t, after)["cipher_b64"]; got != originalCipher {
		t.Errorf("被拒绝的 PUT 不应改动密文：got %v, want %q", got, originalCipher)
	}
}
