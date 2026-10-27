package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 16 字节全零盐的 base64，仅用于测试助记问题接口。
const questionTestSalt = "AAAAAAAAAAAAAAAAAAAAAA=="

func putQuestion(t *testing.T, handler http.Handler, token, question string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(map[string]string{"question": question})
	if err != nil {
		t.Fatalf("marshal question body: %v", err)
	}
	return apiRequest(t, handler, http.MethodPut, "/api/master-password/question", token, string(body))
}

func getMasterConfig(t *testing.T, handler http.Handler) map[string]any {
	t.Helper()

	rec := apiRequest(t, handler, http.MethodGet, "/api/master-password", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var config map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &config); err != nil {
		t.Fatalf("decode master config: %v (%s)", err, rec.Body.String())
	}
	return config
}

func TestUpdateMasterQuestionRoute(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	missing := putQuestion(t, handler, token, "第一份工作？")
	if body := errorBody(t, missing); missing.Code != http.StatusConflict || body.Error.Code != "master_password_missing" {
		t.Fatalf("未配置主密码: expected 409 master_password_missing, got %d %q", missing.Code, missing.Body.String())
	}

	setBody, err := json.Marshal(map[string]string{"salt_b64": questionTestSalt, "question": "你的生日是多少？"})
	if err != nil {
		t.Fatalf("marshal set body: %v", err)
	}
	if rec := apiRequest(t, handler, http.MethodPost, "/api/master-password", token, string(setBody)); rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	before := getMasterConfig(t, handler)
	saltBefore, _ := before["salt_b64"].(string)

	updated := putQuestion(t, handler, token, "第一份工作？")
	if updated.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", updated.Code, updated.Body.String())
	}
	if got := decodeNote(t, updated)["question"]; got != "第一份工作？" {
		t.Errorf("响应 question = %v, want %q", got, "第一份工作？")
	}

	after := getMasterConfig(t, handler)
	if after["question"] != "第一份工作？" {
		t.Errorf("持久化 question = %v, want 新值", after["question"])
	}
	if saltAfter, _ := after["salt_b64"].(string); saltAfter != saltBefore || saltAfter == "" {
		t.Errorf("改助记问题不得换盐：%q -> %q", saltBefore, saltAfter)
	}

	for _, invalid := range []string{"", "   ", strings.Repeat("问", 101)} {
		rec := putQuestion(t, handler, token, invalid)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("问题 %q: expected 400, got %d: %s", firstRunes(invalid, 12), rec.Code, rec.Body.String())
		}
		if field := errorBody(t, rec).Error.Fields["question"]; field == "" {
			t.Errorf("问题 %q: expected fields.question 提示，got %+v", firstRunes(invalid, 12), errorBody(t, rec).Error)
		}
	}
	// 被拒绝的写入必须原样保留旧问题，界面不能显示成「已清空」
	kept := getMasterConfig(t, handler)
	if kept["question"] != "第一份工作？" {
		t.Errorf("被拒绝后 question = %v, want 原问题仍在", kept["question"])
	}

	noCSRF := putQuestion(t, handler, "", "无令牌的问题")
	if body := errorBody(t, noCSRF); noCSRF.Code != http.StatusForbidden || body.Error.Code != "csrf_invalid" {
		t.Errorf("无 CSRF 头: expected 403 csrf_invalid, got %d %q", noCSRF.Code, noCSRF.Body.String())
	}
}

func TestMasterPasswordExistsCopy(t *testing.T) {
	handler, _ := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	setBody, err := json.Marshal(map[string]string{"salt_b64": questionTestSalt, "question": "你的生日是多少？"})
	if err != nil {
		t.Fatalf("marshal set body: %v", err)
	}
	if rec := apiRequest(t, handler, http.MethodPost, "/api/master-password", token, string(setBody)); rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec := apiRequest(t, handler, http.MethodPost, "/api/master-password", token, string(setBody))
	body := errorBody(t, rec)
	if rec.Code != http.StatusConflict || body.Error.Code != "master_password_exists" {
		t.Fatalf("expected 409 master_password_exists, got %d %q", rec.Code, rec.Body.String())
	}
	if strings.Contains(body.Error.Message, "无法再次修改") {
		t.Errorf("文案已过期（主密码可以在信息页修改）：%q", body.Error.Message)
	}
	if !strings.Contains(body.Error.Message, "信息页") && !strings.Contains(body.Error.Message, "/info") {
		t.Errorf("文案应把用户指向信息页，got %q", body.Error.Message)
	}
}

func firstRunes(value string, n int) string {
	runes := []rune(value)
	if len(runes) <= n {
		return value
	}
	return string(runes[:n])
}
