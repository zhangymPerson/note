package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newFakeServer 起一个只回 /api/info 与 /api/shutdown 的假服务，
// 记录收到的关停令牌与关停次数。
func newFakeServer(t *testing.T, token string) (addr string, gotToken chan string, shutdownCalls chan struct{}) {
	t.Helper()
	gotToken = make(chan string, 1)
	shutdownCalls = make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"addr": "127.0.0.1:8080", "started_at": "2026-10-28T09:12:33Z",
			"uptime_seconds": 187, "build_label": "1.5.0 (418917b)", "db_path": "/data/notes.db",
		})
	})
	mux.HandleFunc("POST /api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		select {
		case gotToken <- r.Header.Get(ShutdownTokenHeader):
		default:
		}
		if r.Header.Get(ShutdownTokenHeader) != token {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		select {
		case shutdownCalls <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusAccepted)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String(), gotToken, shutdownCalls
}

func TestProbeReturnsServerInfo(t *testing.T) {
	addr, _, _ := newFakeServer(t, "tok")
	info, err := Probe(addr)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.StartedAt != "2026-10-28T09:12:33Z" || info.UptimeSeconds != 187 ||
		info.Version != "1.5.0 (418917b)" || info.DBPath != "/data/notes.db" || info.Addr != "127.0.0.1:8080" {
		t.Fatalf("info = %+v", info)
	}
}

func TestProbeFailsWhenNothingListens(t *testing.T) {
	if _, err := Probe("127.0.0.1:1"); err == nil {
		t.Fatal("没有服务在监听时应返回错误")
	}
}

func TestRequestStopSendsTokenAndSucceeds(t *testing.T) {
	addr, gotToken, calls := newFakeServer(t, "secret-token")
	if err := RequestStop(State{Addr: addr, Token: "secret-token"}); err != nil {
		t.Fatalf("request stop: %v", err)
	}
	if got := <-gotToken; got != "secret-token" {
		t.Fatalf("服务端收到令牌 %q", got)
	}
	select {
	case <-calls:
	default:
		t.Fatal("关停回调未被触发")
	}
}

func TestRequestStopRejectsWrongToken(t *testing.T) {
	addr, _, _ := newFakeServer(t, "secret-token")
	err := RequestStop(State{Addr: addr, Token: "wrong"})
	if err == nil || !strings.Contains(err.Error(), "关停令牌无效") {
		t.Fatalf("err = %v, want 关停令牌无效", err)
	}
}
