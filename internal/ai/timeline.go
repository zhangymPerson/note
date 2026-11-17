package ai

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"
)

// timeline 采集器：把 httptrace 的钩子写进 Timeline。
//
// 为什么需要它：「模型 60 秒没响应」可能是 DNS 卡住、TCP 连不上、TLS 握手失败、
// 代理挂掉，也可能是连上了模型就是不出字——四种故障的处理方式完全不同。
// 有了这几个时间点，日志一眼能分开。

// httptraceWith 给 ctx 挂上采集钩子。钩子里只记时间点，不做任何 IO。
func httptraceWith(ctx context.Context, t *Timeline) context.Context {
	var (
		mu        sync.Mutex
		dnsStart  time.Time
		connStart time.Time
	)
	set := func(fn func()) {
		mu.Lock()
		defer mu.Unlock()
		fn()
	}
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			mu.Lock()
			dnsStart = time.Now()
			mu.Unlock()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			mu.Lock()
			start := dnsStart
			mu.Unlock()
			if !start.IsZero() {
				set(func() { t.dnsDone = time.Now() })
			}
		},
		ConnectStart: func(_, _ string) {
			mu.Lock()
			connStart = time.Now()
			mu.Unlock()
		},
		ConnectDone: func(_, _ string, err error) {
			mu.Lock()
			start := connStart
			mu.Unlock()
			if err == nil && !start.IsZero() {
				set(func() { t.connDone = time.Now() })
			}
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err == nil {
				set(func() { t.tlsDone = time.Now() })
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			set(func() {
				t.gotConn = time.Now()
				t.reused = info.Reused
			})
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			set(func() { t.wroteReq = time.Now() })
		},
		GotFirstResponseByte: func() {
			set(func() { t.firstByte = time.Now() })
		},
	}
	return httptrace.WithClientTrace(ctx, trace)
}
