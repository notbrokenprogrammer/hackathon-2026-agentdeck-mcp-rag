package server

import (
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
)

type Routes interface{ RegisterRoutes(*http.ServeMux) }

func New(files fs.FS, modules ...Routes) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/host", func(w http.ResponseWriter, r *http.Request) {
		if public := PublicURL(r); public != "" {
			JSON(w, 200, map[string]any{"urls": []string{public}, "public_url": public, "https": true})
			return
		}
		_, port, err := net.SplitHostPort(r.Host)
		if err != nil {
			port = "80"
			if r.TLS != nil {
				port = "443"
			}
		}
		scheme := "http://"
		if r.TLS != nil {
			scheme = "https://"
		}
		urls := []string{}
		if addresses, err := net.InterfaceAddrs(); err == nil {
			for _, a := range addresses {
				ip, _, err := net.ParseCIDR(a.String())
				if err == nil && ip.To4() != nil && ip.IsPrivate() {
					urls = append(urls, scheme+net.JoinHostPort(ip.String(), port))
				}
			}
		}
		JSON(w, 200, map[string]any{"urls": urls})
	})
	for _, module := range modules {
		module.RegisterRoutes(mux)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, 404, map[string]string{"code": "not_found", "message": "Unknown API route"})
	})
	mux.Handle("/", static(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if (r.Method != "GET" && r.Method != "HEAD") && !SameOrigin(r) {
			JSON(w, 403, map[string]string{"code": "forbidden_origin", "message": "Same-origin requests required"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// No wildcard CORS. Native clients without Origin are allowed; browsers must use
// the same scheme and Host, including the port. Forwarded headers are not trusted.
func SameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme+"://"+u.Host, EffectiveOrigin(r)) && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "" && u.RawPath == ""
}

func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func static(files fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if name == "" {
			name = "index.html"
		}
		info, err := fs.Stat(files, name)
		if err == nil && !info.IsDir() {
			if name == "index.html" {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		// Missing API/assets must never receive index.html with HTTP 200.
		if path.Ext(name) != "" || strings.HasPrefix(name, "api/") || name == "api" || name == "ws" || strings.HasPrefix(name, "ws/") || !strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.NotFound(w, r)
			return
		}
		b, err := fs.ReadFile(files, "index.html")
		if err != nil {
			http.Error(w, "Client unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method != "HEAD" {
			_, _ = w.Write(b)
		}
	})
}
