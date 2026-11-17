package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimelineRecordsNetworkPhases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, APIKey: "sk-test-key-123456", Models: []string{"m1"}, HTTP: srv.Client()}
	c.Timeline = NewTimeline()
	if _, err := c.Optimize(context.Background(), Request{Content: "正文"}); err != nil {
		t.Fatalf("Optimize: %v", err)
	}

	got := attrMap(c.Timeline.Attrs())
	for _, key := range []string{"connect_ms", "got_conn_ms", "wrote_request_ms", "first_byte_ms"} {
		if _, ok := got[key].(int64); !ok {
			t.Fatalf("%s 应当有实测值，得到 %v", key, got[key])
		}
	}
	// httptest 用 127.0.0.1 直连：没有 DNS 阶段，也没有 TLS 阶段，必须写 off 而不是 0。
	if got["dns_ms"] != "off" || got["tls_ms"] != "off" {
		t.Errorf("dns_ms/tls_ms 应为 off：%v / %v", got["dns_ms"], got["tls_ms"])
	}
	if got["conn_reused"] != false {
		t.Errorf("第一次请求不该复用连接：%v", got["conn_reused"])
	}
	if first, _ := got["first_byte_ms"].(int64); first < 20 {
		t.Errorf("首字节应当 ≥ 上游延迟 20ms，得到 %d", first)
	}
}

func TestTimelineMarksReusedConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, APIKey: "sk-test-key-123456", Models: []string{"m1"}, HTTP: srv.Client()}
	for i := 0; i < 2; i++ {
		c.Timeline = NewTimeline()
		if _, err := c.Optimize(context.Background(), Request{Content: "正文"}); err != nil {
			t.Fatalf("Optimize: %v", err)
		}
	}
	// 第二次命中 keep-alive：这句 true 就是「共享 client 省掉握手」的证据。
	if got := attrMap(c.Timeline.Attrs())["conn_reused"]; got != true {
		t.Fatalf("第二次请求应当复用连接，得到 %v", got)
	}
}
