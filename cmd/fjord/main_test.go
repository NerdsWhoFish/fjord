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

func TestBrowserAddress(t *testing.T) {
	host, origin, err := browserAddress("0.0.0.0:4727", "", "https://fjord.stout.zone")
	if err != nil || host != "fjord.stout.zone" || origin != "https://fjord.stout.zone" {
		t.Fatalf("public browser address: %q %q %v", host, origin, err)
	}
	host, origin, err = browserAddress("0.0.0.0:4727", "192.168.1.10", "")
	if err != nil || host != "192.168.1.10:4727" || origin != "http://192.168.1.10:4727" {
		t.Fatalf("LAN browser address: %q %q %v", host, origin, err)
	}
	if _, _, err := browserAddress("0.0.0.0:4727", "192.168.1.10", "https://fjord.stout.zone"); err == nil {
		t.Fatal("accepted ambiguous public URL and host")
	}
}

func TestPublicModeConfiguration(t *testing.T) {
	for _, value := range []string{"", "false", "true", "TRUE", "wrong"} {
		t.Setenv("FJORD_PUBLIC_MODE", value)
		enabled, err := publicModeFromEnv()
		if value == "wrong" {
			if err == nil {
				t.Fatal("invalid public mode silently fell back to shared credentials")
			}
		} else if err != nil || enabled != (value == "true" || value == "TRUE") {
			t.Fatalf("unexpected public mode parse for %q", value)
		}
	}
	for _, key := range []string{"FJORD_GO_MODULE_CACHE", "FJORD_LANGUAGE_BUNDLE", "FJORD_NODE_RUNTIME_PATHS"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "/private/runtime")
			if validatePublicRuntime(true, "") == nil || validatePublicRuntime(false, "") != nil {
				t.Fatal("public runtime accepted shared file access or broke private mode")
			}
		})
	}
	if validatePublicRuntime(true, "/operator/config.yaml") == nil {
		t.Fatal("public mode accepted custom mounts")
	}
	for _, origin := range []string{"https://fjord.habibi.tools", "http://localhost:4727", "http://127.0.0.1:4727", "http://[::1]:4727"} {
		if validatePublicOrigin(true, origin) != nil {
			t.Fatalf("rejected safe origin %q", origin)
		}
	}
	if validatePublicOrigin(true, "http://fjord.habibi.tools") == nil {
		t.Fatal("public credentials would travel without TLS")
	}
}
