package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type accessKey struct{}
type requestAccess struct{ public, origin string }

// PublicAccess trusts only a configured HTTPS origin through a local proxy.
// Forwarded headers alone never grant HTTPS, cookie or Origin privileges.
type PublicAccess struct {
	mu            sync.RWMutex
	public        string
	trustLoopback bool
}

func ValidatePublicURL(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", fmt.Errorf("PUBLIC_URL must be an HTTPS origin without a path, credentials or query")
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		address, err := netip.ParseAddr(host)
		if err != nil || address.Zone() != "" {
			return "", fmt.Errorf("PUBLIC_URL must contain a valid IPv6 address")
		}
		host = "[" + address.String() + "]"
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", fmt.Errorf("PUBLIC_URL port must be between 1 and 65535")
		}
		// Browsers remove HTTPS's default port from Host and Origin. Keep the
		// configured proxy origin in the same canonical form.
		if number != 443 {
			host += ":" + strconv.Itoa(number)
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return "", fmt.Errorf("PUBLIC_URL must not contain an empty port")
	}
	return "https://" + host, nil
}

func NewPublicAccess(public string, trustLoopback bool) (*PublicAccess, error) {
	p := &PublicAccess{trustLoopback: trustLoopback}
	return p, p.SetPublicURL(public)
}
func (p *PublicAccess) SetPublicURL(raw string) error {
	value, err := ValidatePublicURL(raw)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.public = value
	p.mu.Unlock()
	return nil
}
func (p *PublicAccess) PublicURL() string { p.mu.RLock(); defer p.mu.RUnlock(); return p.public }

func directOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + strings.ToLower(r.Host)
}
func EffectiveOrigin(r *http.Request) string {
	if access, ok := r.Context().Value(accessKey{}).(requestAccess); ok {
		return access.origin
	}
	return directOrigin(r)
}
func PublicURL(r *http.Request) string {
	access, _ := r.Context().Value(accessKey{}).(requestAccess)
	return access.public
}
func IsHTTPS(r *http.Request) bool { return strings.HasPrefix(EffectiveOrigin(r), "https://") }

func (p *PublicAccess) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// MCP capabilities are never routed through the public app, even when a
		// reverse proxy makes the TCP peer appear local.
		if r.URL.Path == "/api/internal" || strings.HasPrefix(r.URL.Path, "/api/internal/") {
			http.NotFound(w, r)
			return
		}
		public := p.PublicURL()
		origin := directOrigin(r)
		proxyOrigin, _ := ValidatePublicURL("https://" + r.Host)
		if public != "" && r.TLS == nil && proxyOrigin == public {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			ip := net.ParseIP(host)
			if !p.trustLoopback || err != nil || ip == nil || !ip.IsLoopback() {
				JSON(w, 403, map[string]string{"code": "untrusted_proxy", "message": "HTTPS proxy connection required"})
				return
			}
			if r.Header.Get("X-Forwarded-Proto") != "https" {
				if r.Method == http.MethodGet || r.Method == http.MethodHead {
					http.Redirect(w, r, public+r.URL.RequestURI(), http.StatusPermanentRedirect)
				} else {
					JSON(w, 403, map[string]string{"code": "https_required", "message": "Open the HTTPS invitation"})
				}
				return
			}
			origin = public
		}
		// Ignore X-Forwarded-Host/Forwarded/X-Forwarded-For; preserve the actual peer.
		r = r.WithContext(context.WithValue(r.Context(), accessKey{}, requestAccess{public: public, origin: origin}))
		next.ServeHTTP(w, r)
	})
}
