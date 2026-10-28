package config

import (
	"net"
	"sort"
	"strings"
)

// AccessURLs 由监听地址推导可直接在浏览器打开的访问地址。
// 回环、具体 IP 或主机名直接拼装；通配地址（0.0.0.0 / :: / 空 host）枚举本机
// 非回环、非链路本地地址，每个地址一条；枚举为空时回退到回环地址，保证至少
// 打印一条可用 URL。端口不做转换（调用方应传入 listener 的实际地址）。
func AccessURLs(addr string) []string {
	return accessURLs(addr, interfaceAddrs)
}

// StartupURLs 组合「请求的监听地址」与「listener 实际地址」推导启动时要打印
// 的访问地址：地址家族取请求 host（0.0.0.0 枚举 IPv4、:: 枚举 IPv6——Windows
// 对 0.0.0.0 监听会建立双栈 socket 并把 Addr() 汇报成 [::]，直接用 listener
// 地址会把该打印的非回环 IPv4 全部漏掉），端口取 listener 实际端口（兼容 :0）。
func StartupURLs(cfgAddr, listenAddr string) []string {
	return startupURLs(cfgAddr, listenAddr, interfaceAddrs)
}

func startupURLs(cfgAddr, listenAddr string, enumerate func() []net.IP) []string {
	host, _, err := net.SplitHostPort(cfgAddr)
	if err != nil {
		return nil
	}
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return nil
	}
	return accessURLs(net.JoinHostPort(host, port), enumerate)
}

// accessURLs 是 AccessURLs 的可注入版本，enumerate 供测试传入假网卡，
// 单测不依赖真实网络接口。
func accessURLs(addr string, enumerate func() []net.IP) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	host = strings.TrimSpace(host)
	if host != "" && host != "0.0.0.0" && host != "::" {
		return []string{"http://" + net.JoinHostPort(host, port)}
	}

	wantV4 := host == "0.0.0.0"
	seen := map[string]bool{}
	var urls []string
	for _, ip := range enumerate() {
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
			continue
		}
		if (ip.To4() != nil) != wantV4 {
			continue
		}
		text := ip.String()
		if seen[text] {
			continue
		}
		seen[text] = true
		urls = append(urls, "http://"+net.JoinHostPort(text, port))
	}
	if len(urls) == 0 {
		if wantV4 {
			urls = append(urls, "http://"+net.JoinHostPort("127.0.0.1", port))
		} else {
			urls = append(urls, "http://"+net.JoinHostPort("::1", port))
		}
	}
	sort.Strings(urls)
	return urls
}

// interfaceAddrs 列出本机所有接口地址；通配监听时用它枚举可访问的具体地址。
func interfaceAddrs() []net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil {
				ips = append(ips, ip)
			}
		}
	}
	return ips
}
