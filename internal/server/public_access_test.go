package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"feeders/internal/server"
)

func TestPublicURLCanonicalPorts(t *testing.T) {
	for raw, want := range map[string]string{
		"":                                    "",
		"https://Crew.Example.Test":           "https://crew.example.test",
		"https://crew.example.test:443/":      "https://crew.example.test",
		"https://crew.example.test:0443":      "https://crew.example.test",
		"https://crew.example.test:08443":     "https://crew.example.test:8443",
		"https://crew.example.test:1":         "https://crew.example.test:1",
		"https://crew.example.test:65535":     "https://crew.example.test:65535",
		"https://[::1]:443":                   "https://[::1]",
		"https://[2001:0DB8:0:0:0:0:0:1]:443": "https://[2001:db8::1]",
		"https://[2001:db8::1]:8443":          "https://[2001:db8::1]:8443",
	} {
		t.Run(raw, func(t *testing.T) {
			got, err := server.ValidatePublicURL(raw)
			if err != nil || got != want {
				t.Fatalf("canonical origin %q: got %q, %v; want %q", raw, got, err, want)
			}
		})
	}
	for _, raw := range []string{"https://host:", "https://host:0", "https://host:000", "https://host:65536", "https://host:99999999999999999999", "https://host:-1", "https://host:abc", "https://[::1]:", "https://[::1]:65536", "https://[not-ipv6]", "https://[fe80::1%25eth0]", "http://host", "https://user:password@host", "https://host/path", "https://host?query=1"} {
		t.Run(raw, func(t *testing.T) {
			if got, err := server.ValidatePublicURL(raw); err == nil {
				t.Fatalf("invalid origin accepted: %q => %q", raw, got)
			}
		})
	}
}

func TestHTTPSProxyDefaultPortNormalization(t *testing.T) {
	for _, tc := range []struct{ public, host, origin string }{
		{"https://crew.example.test:443", "crew.example.test", "https://crew.example.test"},
		{"https://crew.example.test:443", "crew.example.test:443", "https://crew.example.test"},
		{"https://[0:0:0:0:0:0:0:1]:443", "[::1]", "https://[::1]"},
		{"https://[::1]:8443", "[::1]:8443", "https://[::1]:8443"},
	} {
		t.Run(tc.public+"/"+tc.host, func(t *testing.T) {
			access, err := server.NewPublicAccess(tc.public, true)
			if err != nil {
				t.Fatal(err)
			}
			handler := access.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !server.IsHTTPS(r) || !server.SameOrigin(r) || server.PublicURL(r) != tc.origin {
					t.Error("canonical proxy origin did not preserve HTTPS and same-origin auth")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/api/auth/login", nil)
			request.RemoteAddr = "127.0.0.1:4411"
			request.Header.Set("X-Forwarded-Proto", "https")
			request.Header.Set("Origin", tc.origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("proxy with an explicit default port rejected: %d", response.Code)
			}
		})
	}
}
