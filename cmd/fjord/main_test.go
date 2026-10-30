package main

import "testing"

func TestRequestHost(t *testing.T) {
	for _, tt := range []struct{ listen, host, want string }{
		{"127.0.0.1:49123", "", "127.0.0.1:49123"},
		{"[::1]:4727", "", "[::1]:4727"},
		{"192.168.1.10:4727", "", "192.168.1.10:4727"},
		{"[::]:4727", "192.168.1.10", "192.168.1.10:4727"},
		{"0.0.0.0:4727", "fjord.local", "fjord.local:4727"},
		{"[::]:80", "::1", "[::1]"},
		{"0.0.0.0:80", "fjord.local", "fjord.local"},
	} {
		got, err := requestHost(tt.listen, tt.host)
		if err != nil || got != tt.want {
			t.Errorf("requestHost(%q, %q) = %q, %v; want %q", tt.listen, tt.host, got, err, tt.want)
		}
	}
	for _, host := range []string{"", "0.0.0.0", "::", "http://fjord.local", "fjord.local:4727", "fjord.local/path", "user@fjord.local", "*", "fjord.local\n"} {
		if _, err := requestHost("0.0.0.0:4727", host); err == nil {
			t.Errorf("accepted invalid browser host %q", host)
		}
	}
}
