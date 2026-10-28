package daemon

import "net"

// ControlAddr 把「服务实际监听的地址」转成「本机可连接的请求地址」。
// 监听通配地址时不能直接拿去发 HTTP 请求：0.0.0.0 在部分系统上会「连上」
// 任意本地监听者，[::] 则根本连不通，因此统一改连回环地址。
// 解析失败时原样返回，由调用方在请求出错时给出可读错误。
func ControlAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0":
		return net.JoinHostPort("127.0.0.1", port)
	case "::":
		return net.JoinHostPort("::1", port)
	default:
		return addr
	}
}
