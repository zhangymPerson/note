package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ShutdownTokenHeader 是关停请求携带令牌的请求头，与 internal/web 侧同名常量保持一致。
const ShutdownTokenHeader = "X-Shutdown-Token"

// probeTimeout 是探测单个实例的上限：本机回环上响应应当是毫秒级，
// 超过两秒基本可以认定「端口上不是我们在跑的 note」。
const probeTimeout = 2 * time.Second

// ServerInfo 是 GET /api/info 中 status 需要展示的字段子集。
type ServerInfo struct {
	Addr          string `json:"addr"`
	StartedAt     string `json:"started_at"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	Version       string `json:"build_label"`
	DBPath        string `json:"db_path"`
}

// Probe 探测给定监听地址上是否有 note 在响应。
// 地址先经 ControlAddr 转成可连接的回环地址，因此对通配监听同样成立。
func Probe(addr string) (ServerInfo, error) {
	url := "http://" + ControlAddr(addr) + "/api/info"
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ServerInfo{}, fmt.Errorf("probe %s: %w", addr, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ServerInfo{}, fmt.Errorf("probe %s: %w", addr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ServerInfo{}, fmt.Errorf("probe %s: HTTP %d", addr, resp.StatusCode)
	}
	var info ServerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return ServerInfo{}, fmt.Errorf("probe %s: decode info: %w", addr, err)
	}
	return info, nil
}

// RequestStop 用状态文件里的地址与令牌请求实例优雅关停。
// 令牌错误与「服务没起来」必须能区分：前者说明状态文件已过期，
// 后者说明进程可能还在但没在服务。
func RequestStop(st State) error {
	url := "http://" + ControlAddr(st.Addr) + "/api/shutdown"
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("request stop: %w", err)
	}
	req.Header.Set(ShutdownTokenHeader, st.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request stop %s: %w", st.Addr, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil
	case http.StatusForbidden:
		return fmt.Errorf("request stop %s: 关停令牌无效（状态文件可能已过期）", st.Addr)
	default:
		return fmt.Errorf("request stop %s: HTTP %d", st.Addr, resp.StatusCode)
	}
}
