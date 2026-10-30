package web

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"note/internal/config"
)

type aiConfigResp struct {
	BaseURL      string `json:"base_url"`
	Prompt       string `json:"prompt"`
	APIKeySet    bool   `json:"api_key_set"`
	APIKeyMasked string `json:"api_key_masked"`
	Path         string `json:"path"`
}

func decodeAIConfig(t *testing.T, body []byte) aiConfigResp {
	t.Helper()
	var out aiConfigResp
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode ai config: %v (%s)", err, body)
	}
	return out
}

func TestAIConfigAPIEmptyBeforeFirstSave(t *testing.T) {
	handler, cfg := newTestHandler(t)

	rec := apiRequest(t, handler, http.MethodGet, "/api/ai-config", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := decodeAIConfig(t, rec.Body.Bytes())
	if body.BaseURL != "" || body.Prompt != "" || body.APIKeySet || body.APIKeyMasked != "" {
		t.Fatalf("首次读取应当全空: %+v", body)
	}
	if body.Path != cfg.ConfigPath {
		t.Fatalf("path = %q, want %q", body.Path, cfg.ConfigPath)
	}
	// 只读接口不能顺手把配置文件建出来
	if _, err := os.Stat(cfg.ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("GET 不该创建配置文件：%v", err)
	}
}

// 响应里永远没有明文密钥：只有掩码与「已配置」标记。
func TestAIConfigAPINeverReturnsPlaintextKey(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	secret := "sk-verysecretvalue1234567890"

	rec := apiRequest(t, handler, http.MethodPut, "/api/ai-config", token, `{
		"base_url":"https://api.openai.com/v1",
		"prompt":"整理成笔记",
		"api_key":"`+secret+`"
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d, want 200: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("PUT 响应泄露了明文密钥: %s", rec.Body)
	}
	body := decodeAIConfig(t, rec.Body.Bytes())
	if !body.APIKeySet || body.APIKeyMasked == "" || strings.Contains(body.APIKeyMasked, "secretvalue") {
		t.Fatalf("PUT 响应 = %+v", body)
	}

	rec = apiRequest(t, handler, http.MethodGet, "/api/ai-config", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("GET 响应泄露了明文密钥: %s", rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("含密钥信息的响应必须 no-store，得到 %q", rec.Header().Get("Cache-Control"))
	}

	// 真的落进了 note.yaml：手改配置与 CLI 读的是同一份
	if _, _, err := config.ReadAI(cfg.ConfigPath); err != nil {
		t.Fatalf("read ai config: %v", err)
	}
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !strings.Contains(string(data), secret) {
		t.Fatalf("密钥没写进配置文件:\n%s", data)
	}
}

func TestAIConfigAPIKeepsKeyWhenFieldOmitted(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	rec := apiRequest(t, handler, http.MethodPut, "/api/ai-config", token,
		`{"base_url":"https://a.example.com/v1","prompt":"p","api_key":"sk-firstversion1234567890"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("首次 PUT = %d: %s", rec.Code, rec.Body)
	}

	// 省略 api_key：地址改了，密钥原样保留
	rec = apiRequest(t, handler, http.MethodPut, "/api/ai-config", token,
		`{"base_url":"https://b.example.com/v1","prompt":"p2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("二次 PUT = %d: %s", rec.Code, rec.Body)
	}

	ai, _, err := config.ReadAI(cfg.ConfigPath)
	if err != nil {
		t.Fatalf("read ai config: %v", err)
	}
	if ai.APIKey != "sk-firstversion1234567890" {
		t.Fatalf("省略 api_key 时不该改动密钥，得到 %q", ai.APIKey)
	}
	if ai.BaseURL != "https://b.example.com/v1" || ai.Prompt != "p2" {
		t.Fatalf("其余两项应当更新: %+v", ai)
	}
}

func TestAIConfigAPIClearsKeyWithEmptyString(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)

	if rec := apiRequest(t, handler, http.MethodPut, "/api/ai-config", token,
		`{"base_url":"","prompt":"","api_key":"sk-tobedeleted1234567890"}`); rec.Code != http.StatusOK {
		t.Fatalf("首次 PUT = %d: %s", rec.Code, rec.Body)
	}

	rec := apiRequest(t, handler, http.MethodPut, "/api/ai-config", token,
		`{"base_url":"","prompt":"","api_key":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("清空 PUT = %d: %s", rec.Code, rec.Body)
	}
	if body := decodeAIConfig(t, rec.Body.Bytes()); body.APIKeySet || body.APIKeyMasked != "" {
		t.Fatalf("清空后应当未配置: %+v", body)
	}

	ai, _, err := config.ReadAI(cfg.ConfigPath)
	if err != nil || ai.APIKey != "" {
		t.Fatalf("密钥应当被清空：%q %v", ai.APIKey, err)
	}
}

func TestAIConfigAPIRejectsBadValues(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		field  string
		suffix string
	}{
		{"地址没协议", `{"base_url":"api.example.com","prompt":""}`, "base_url", "http"},
		{"地址协议不对", `{"base_url":"ftp://example.com","prompt":""}`, "base_url", "http"},
		{"密钥带空格", `{"base_url":"","prompt":"","api_key":"sk-with space"}`, "api_key", "空白"},
		{"提示词超长", `{"base_url":"","prompt":"` + strings.Repeat("字", config.MaxAIPromptRunes+1) + `"}`, "prompt", "2000"},
	}
	for _, tc := range cases {
		handler, cfg := newTestHandler(t)
		token := csrfTokenFor(t, handler)

		rec := apiRequest(t, handler, http.MethodPut, "/api/ai-config", token, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s：PUT = %d, want 400: %s", tc.name, rec.Code, rec.Body)
		}
		var errBody apiErrorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
			t.Fatalf("%s：decode error: %v", tc.name, err)
		}
		if errBody.Error.Code != "validation_failed" || errBody.Error.Fields[tc.field] == "" {
			t.Fatalf("%s：应当标红 %s，得到 %+v", tc.name, tc.field, errBody.Error)
		}
		if !strings.Contains(errBody.Error.Fields[tc.field], tc.suffix) {
			t.Fatalf("%s：字段提示应当含 %q，得到 %q", tc.name, tc.suffix, errBody.Error.Fields[tc.field])
		}
		// 校验失败不该写文件
		if _, err := os.Stat(cfg.ConfigPath); !os.IsNotExist(err) {
			t.Fatalf("%s：校验失败不该写配置文件", tc.name)
		}
	}
}

// 配置文件里已有严格解析不认识的键时，写入会把它们带出去，所以拒绝并返回 409。
func TestAIConfigAPIConflictsOnUnknownKeys(t *testing.T) {
	handler, cfg := newTestHandler(t)
	token := csrfTokenFor(t, handler)
	if err := os.WriteFile(cfg.ConfigPath, []byte("server:\n  addr: 127.0.0.1:8080\nbogus: 1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rec := apiRequest(t, handler, http.MethodPut, "/api/ai-config", token,
		`{"base_url":"https://api.openai.com/v1","prompt":""}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("PUT = %d, want 409: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "note.yaml") {
		t.Fatalf("提示应当指向配置文件: %s", rec.Body)
	}
}

func TestAIConfigAPIRequiresCSRF(t *testing.T) {
	handler, cfg := newTestHandler(t)

	rec := apiRequest(t, handler, http.MethodPut, "/api/ai-config", "",
		`{"base_url":"https://api.openai.com/v1","prompt":""}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无令牌的 PUT = %d, want 403", rec.Code)
	}
	if _, err := os.Stat(cfg.ConfigPath); !os.IsNotExist(err) {
		t.Fatal("被拒的请求不该写配置文件")
	}
}
