package crawl

import (
	"net/netip"
	"net/url"
	"testing"
)

func TestNormalize(t *testing.T) {
	base, _ := url.Parse("https://Example.com/blog/post")
	tests := []struct {
		ref, want string
		ok        bool
	}{
		{"", "", false},
		{"#top", "", false},
		{"mailto:a@b.c", "", false},
		{"javascript:void(0)", "", false},
		{"https://user:pw@example.com/", "", false},
		{"other", "https://example.com/blog/other", true},
		{"/x#frag", "https://example.com/x", true},
		{"HTTPS://EXAMPLE.COM:443", "https://example.com/", true},
		{"http://example.com:80/a?b=1", "http://example.com/a?b=1", true},
		{"http://example.com:8080/a", "http://example.com:8080/a", true},
	}
	for _, tt := range tests {
		got, ok := Normalize(base, tt.ref)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Normalize(%q) = %q %v, want %q %v", tt.ref, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSameSite(t *testing.T) {
	root, _ := url.Parse("https://example.com/")
	for raw, want := range map[string]bool{
		"https://www.example.com/a": true,
		"http://example.com/a":      true,
		"https://blog.example.com/": false,
		"https://example.org/":      false,
	} {
		u, _ := url.Parse(raw)
		if sameSite(root, u) != want {
			t.Errorf("sameSite(%s) = %v", raw, !want)
		}
	}
}

func TestInternalAddresses(t *testing.T) {
	for raw, want := range map[string]bool{
		"127.0.0.1": true, "10.1.2.3": true, "192.168.1.1": true, "172.16.0.1": true, "169.254.169.254": true,
		"100.64.0.1": true, "0.0.0.0": true, "::1": true, "fd00::1": true, "fe80::1": true, "::ffff:127.0.0.1": true,
		"93.184.216.34": false, "2606:4700::1111": false, "1.1.1.1": false,
	} {
		if got := internal(netip.MustParseAddr(raw)); got != want {
			t.Errorf("internal(%s) = %v, want %v", raw, got, want)
		}
	}
}
