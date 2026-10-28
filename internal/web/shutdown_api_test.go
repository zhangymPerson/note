package web

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newControlHandler 装配一个启用关停端点的 handler，并记录回调次数。
func newControlHandler(t *testing.T, token string) (http.Handler, *int32) {
	t.Helper()
	var calls int32
	handler, _ := newTestHandlerWithControl(t, Control{
		Token:    token,
		Shutdown: func() { atomic.AddInt32(&calls, 1) },
	})
	return handler, &calls
}

func TestShutdownAcceptsValidTokenWithoutCSRFCookie(t *testing.T) {
	handler, calls := newControlHandler(t, "secret")
	req := httptest.NewRequest(http.MethodPost, shutdownPath, nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set(shutdownTokenHeader, "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	// 回调是异步触发的（handler 先回 202），轮询等它落地。
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(calls) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("关停回调未被触发")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("关停回调调用 %d 次，want 1", got)
	}
}

func TestShutdownRejectsWrongOrMissingToken(t *testing.T) {
	handler, calls := newControlHandler(t, "secret")
	for _, token := range []string{"", "wrong"} {
		req := httptest.NewRequest(http.MethodPost, shutdownPath, nil)
		req.Host = "127.0.0.1:8080"
		if token != "" {
			req.Header.Set(shutdownTokenHeader, token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("token %q: expected 403, got %d", token, rec.Code)
		}
		if body := errorBody(t, rec); body.Error.Code != "shutdown_denied" {
			t.Fatalf("token %q: error code = %q, want shutdown_denied", token, body.Error.Code)
		}
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Fatal("令牌无效时不应触发关停")
	}
}

func TestShutdownDisabledWithoutControl(t *testing.T) {
	handler, _ := newTestHandler(t) // 无 Control：等价于前台模式
	req := httptest.NewRequest(http.MethodPost, shutdownPath, nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set(shutdownTokenHeader, "anything")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if body := errorBody(t, rec); body.Error.Code != "shutdown_disabled" {
		t.Fatalf("error code = %q, want shutdown_disabled", body.Error.Code)
	}
}

func TestShutdownRejectsNonPostAndForeignHost(t *testing.T) {
	handler, _ := newControlHandler(t, "secret")
	get := doRequest(handler, http.MethodGet, shutdownPath, "127.0.0.1:8080", "")
	if get.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: expected 405, got %d", get.Code)
	}

	req := httptest.NewRequest(http.MethodPost, shutdownPath, nil)
	req.Host = "evil.example:8080"
	req.Header.Set(shutdownTokenHeader, "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("外站 Host: expected 400 host_invalid, got %d: %s", rec.Code, rec.Body.String())
	}
}
