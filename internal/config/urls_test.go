package config

import (
	"net"
	"reflect"
	"testing"
)

func fakeAddrs(ips ...string) func() []net.IP {
	return func() []net.IP {
		out := make([]net.IP, 0, len(ips))
		for _, s := range ips {
			out = append(out, net.ParseIP(s))
		}
		return out
	}
}

func TestAccessURLsDirectHost(t *testing.T) {
	cases := []struct {
		name string
		addr string
		want []string
	}{
		{"loopback ipv4", "127.0.0.1:8080", []string{"http://127.0.0.1:8080"}},
		{"hostname", "localhost:8080", []string{"http://localhost:8080"}},
		{"specific ipv4", "192.168.1.5:9090", []string{"http://192.168.1.5:9090"}},
		{"loopback ipv6", "[::1]:8080", []string{"http://[::1]:8080"}},
		{"specific ipv6", "[fd00::a]:8080", []string{"http://[fd00::a]:8080"}},
		{"missing port", "127.0.0.1", nil},
		{"not an address", "nope", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AccessURLs(tc.addr); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("AccessURLs(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func TestAccessURLsWildcardIPv4EnumeratesNonLoopback(t *testing.T) {
	got := accessURLs("0.0.0.0:9090", fakeAddrs("127.0.0.1", "192.168.1.5", "10.0.0.2", "169.254.100.7", "fd00::a"))
	want := []string{"http://10.0.0.2:9090", "http://192.168.1.5:9090"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accessURLs(0.0.0.0) = %v, want %v", got, want)
	}
}

func TestAccessURLsWildcardIPv4Deduplicates(t *testing.T) {
	got := accessURLs("0.0.0.0:9090", fakeAddrs("10.0.0.2", "10.0.0.2", "::ffff:10.0.0.2"))
	want := []string{"http://10.0.0.2:9090"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accessURLs(0.0.0.0) = %v, want %v", got, want)
	}
}

func TestAccessURLsWildcardIPv4FallsBackToLoopback(t *testing.T) {
	got := accessURLs("0.0.0.0:9090", fakeAddrs("127.0.0.1"))
	want := []string{"http://127.0.0.1:9090"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accessURLs(0.0.0.0) = %v, want %v", got, want)
	}
}

func TestAccessURLsWildcardIPv6EnumeratesNonLoopback(t *testing.T) {
	got := accessURLs("[::]:9090", fakeAddrs("fe80::1", "fd00::a", "::1", "192.168.1.5"))
	want := []string{"http://[fd00::a]:9090"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accessURLs([::]) = %v, want %v", got, want)
	}
}

func TestAccessURLsWildcardIPv6FallsBackToLoopback(t *testing.T) {
	got := accessURLs("[::]:9090", fakeAddrs("127.0.0.1"))
	want := []string{"http://[::1]:9090"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accessURLs([::]) = %v, want %v", got, want)
	}
}

func TestAccessURLsEmptyHostTreatedAsWildcard(t *testing.T) {
	got := accessURLs(":8080", fakeAddrs("192.168.1.5", "fd00::a"))
	want := []string{"http://[fd00::a]:8080"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accessURLs(:8080) = %v, want %v", got, want)
	}
}

func TestStartupURLsTakesFamilyFromRequestAndPortFromListener(t *testing.T) {
	// Windows 对 0.0.0.0 监听会建立双栈 socket，listener 汇报 [::]；
	// 家族必须取请求地址，否则枚举成 IPv6、把可用的非回环 IPv4 全部漏掉。
	got := startupURLs("0.0.0.0:0", "[::]:8100", fakeAddrs("192.168.1.5", "fd00::a"))
	want := []string{"http://192.168.1.5:8100"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("startupURLs(0.0.0.0, [::]) = %v, want %v", got, want)
	}
}

func TestStartupURLsSpecificHostKeepsRequestHost(t *testing.T) {
	got := startupURLs("localhost:0", "127.0.0.1:8099", fakeAddrs("192.168.1.5"))
	want := []string{"http://localhost:8099"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("startupURLs(localhost) = %v, want %v", got, want)
	}
}

func TestStartupURLsWildcardV6UsesActualPort(t *testing.T) {
	got := startupURLs("[::]:0", "[::]:8100", fakeAddrs("fd00::a", "192.168.1.5"))
	want := []string{"http://[fd00::a]:8100"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("startupURLs([::]) = %v, want %v", got, want)
	}
}

func TestStartupURLsBadInput(t *testing.T) {
	if got := startupURLs("nope", "[::]:8080", fakeAddrs("192.168.1.5")); got != nil {
		t.Fatalf("startupURLs(bad request addr) = %v, want nil", got)
	}
	if got := startupURLs("127.0.0.1:8080", "nope", fakeAddrs("192.168.1.5")); got != nil {
		t.Fatalf("startupURLs(bad listen addr) = %v, want nil", got)
	}
}
