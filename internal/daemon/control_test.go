package daemon

import "testing"

func TestControlAddrMapsWildcardsToLoopback(t *testing.T) {
	cases := []struct{ in, want string }{
		{"127.0.0.1:8080", "127.0.0.1:8080"},
		{"localhost:8080", "localhost:8080"},
		{"[::1]:8080", "[::1]:8080"},
		{"0.0.0.0:8080", "127.0.0.1:8080"},
		{":8080", "127.0.0.1:8080"},
		{"[::]:8080", "[::1]:8080"},
		{"192.168.1.5:8080", "192.168.1.5:8080"},
		{"garbage", "garbage"},
	}
	for _, tc := range cases {
		if got := ControlAddr(tc.in); got != tc.want {
			t.Errorf("ControlAddr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
