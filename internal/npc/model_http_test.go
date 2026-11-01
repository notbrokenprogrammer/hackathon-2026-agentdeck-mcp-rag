package npc

import (
	"net/http"
	"net/http/httptest"
)

// The real Ollama client still builds/parses HTTP. Only the transport runs in
// memory, so these protocol tests work without an installed model or TCP ports.
type modelHTTP struct {
	URL     string
	handler http.Handler
}

func newModelHTTP(handler http.Handler) *modelHTTP {
	return &modelHTTP{URL: "http://127.0.0.1:11434", handler: handler}
}
func (h *modelHTTP) Close() {}
func (h *modelHTTP) RoundTrip(req *http.Request) (*http.Response, error) {
	done := make(chan *http.Response, 1)
	go func() {
		recorder := httptest.NewRecorder()
		h.handler.ServeHTTP(recorder, req)
		done <- recorder.Result()
	}()
	select {
	case response := <-done:
		return response, nil
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
}
