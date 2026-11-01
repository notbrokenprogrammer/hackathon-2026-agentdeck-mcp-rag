//go:build qatools

package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"feeders/internal/auth"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// Test-only fixture; never compiled into release binaries.
func qaHandler(h http.Handler, a *auth.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__qa/viewport" {
			width := "390"
			if r.URL.Query().Get("width") == "1366" {
				width = "1366"
			}
			if r.URL.Query().Get("width") == "1920" {
				width = "1920"
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>QA viewport %s</title></head><body style="margin:0;background:#18232d"><iframe title="Тестовый экран игры" src="/" style="width:%spx;height:844px;border:0;display:block"></iframe></body></html>`, width, width)
			return
		}
		if r.URL.Path != "/__qa/enter" {
			h.ServeHTTP(w, r)
			return
		}
		var bytes [16]byte
		rand.Read(bytes[:])
		payload, _ := json.Marshal(map[string]string{"username": "qa_" + hex.EncodeToString(bytes[:4]), "password": hex.EncodeToString(bytes[:])})
		req := httptest.NewRequest("POST", "/api/auth/register", strings.NewReader(string(payload)))
		rec := httptest.NewRecorder()
		a.Register(rec, req)
		if rec.Code != 201 {
			http.Error(w, "QA fixture unavailable", 500)
			return
		}
		for _, cookie := range rec.Result().Cookies() {
			http.SetCookie(w, cookie)
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
}
